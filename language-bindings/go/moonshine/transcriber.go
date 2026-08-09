package moonshine

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/moonshine-ai/moonshine/language-bindings/go/raw"
)

type transcriberBindings interface {
	loadTranscriberFromFiles(path string, modelArch uint32, options []Option) int32
	freeTranscriber(handle int32)
	errorToString(code int32) string
}

type rawTranscriberBindings struct{}

func (rawTranscriberBindings) loadTranscriberFromFiles(path string, modelArch uint32, options []Option) int32 {
	converted := rawOptions(options)
	return raw.MoonshineLoadTranscriberFromFiles(
		path,
		modelArch,
		converted,
		uint64(len(converted)),
		raw.MoonshineHeaderVersion,
	)
}

func (rawTranscriberBindings) freeTranscriber(handle int32) {
	raw.MoonshineFreeTranscriber(handle)
}

func (rawTranscriberBindings) errorToString(code int32) string {
	return copyCString(raw.MoonshineErrorToString(code))
}

func copyCString(pointer *byte) string {
	if pointer == nil {
		return ""
	}

	bytes := make([]byte, 0, 64)
	for offset := uintptr(0); ; offset++ {
		value := *(*byte)(unsafe.Add(unsafe.Pointer(pointer), offset))
		if value == 0 {
			return string(bytes)
		}
		bytes = append(bytes, value)
	}
}

// Transcriber owns a native Moonshine transcriber handle.
//
// Call Close when the transcriber is no longer needed. A finalizer releases
// abandoned transcribers as a fallback, but callers should not rely on its
// timing. A Transcriber must not be copied after first use.
type Transcriber struct {
	bindings  transcriberBindings
	handle    int32
	closeOnce sync.Once
}

// NewTranscriber loads a transcriber from model files beneath modelPath.
func NewTranscriber(modelPath string, modelArch ModelArch, options ...Option) (*Transcriber, error) {
	return newTranscriber(rawTranscriberBindings{}, modelPath, modelArch, options...)
}

func newTranscriber(bindings transcriberBindings, modelPath string, modelArch ModelArch, options ...Option) (*Transcriber, error) {
	if err := validateOptions(options); err != nil {
		return nil, err
	}
	if strings.IndexByte(modelPath, 0) >= 0 {
		return nil, fmt.Errorf("model path contains a NUL: %w", ErrInvalidArgument)
	}

	handle := bindings.loadTranscriberFromFiles(modelPath, uint32(modelArch), options)
	if handle < 0 {
		return nil, fmt.Errorf(
			"moonshine: load transcriber from %q: %w",
			modelPath,
			nativeError(handle, bindings.errorToString(handle)),
		)
	}

	transcriber := &Transcriber{
		bindings: bindings,
		handle:   handle,
	}
	runtime.SetFinalizer(transcriber, (*Transcriber).finalize)
	return transcriber, nil
}

// Close releases the native transcriber. It is safe to call Close more than
// once.
func (t *Transcriber) Close() {
	if t == nil {
		return
	}

	t.closeOnce.Do(func() {
		runtime.SetFinalizer(t, nil)
		t.bindings.freeTranscriber(t.handle)
	})
}

func (t *Transcriber) finalize() {
	t.Close()
}
