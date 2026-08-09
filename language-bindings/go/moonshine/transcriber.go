package moonshine

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"github.com/moonshine-ai/moonshine/language-bindings/go/raw"
)

type transcriberBindings interface {
	loadTranscriberFromFiles(path string, modelArch uint32, options []Option) int32
	loadTranscriberFromMemoryFiles(
		filenames []string,
		memory [][]byte,
		memorySizes []uint64,
		modelArch uint32,
		options []Option,
	) int32
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

func (rawTranscriberBindings) loadTranscriberFromMemoryFiles(
	filenames []string,
	memory [][]byte,
	memorySizes []uint64,
	modelArch uint32,
	options []Option,
) int32 {
	converted := rawOptions(options)
	return raw.MoonshineLoadTranscriberFromMemoryFiles(
		filenames,
		memory,
		memorySizes,
		uint64(len(filenames)),
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
	memory    [][]byte
	pinner    *runtime.Pinner
	closeOnce sync.Once
}

// NewTranscriber loads a transcriber from model files beneath modelPath.
func NewTranscriber(modelPath string, modelArch ModelArch, options ...Option) (*Transcriber, error) {
	return newTranscriber(rawTranscriberBindings{}, modelPath, modelArch, options...)
}

// NewTranscriberFromMemory loads a transcriber from model assets keyed by
// their canonical filenames. Non-empty buffers are pinned until Close because
// the native library retains pointers to them for the transcriber's lifetime.
// Callers must not modify the buffers until the transcriber is closed.
func NewTranscriberFromMemory(files map[string][]byte, modelArch ModelArch, options ...Option) (*Transcriber, error) {
	return newTranscriberFromMemory(rawTranscriberBindings{}, files, modelArch, options...)
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

func newTranscriberFromMemory(
	bindings transcriberBindings,
	files map[string][]byte,
	modelArch ModelArch,
	options ...Option,
) (*Transcriber, error) {
	if err := validateOptions(options); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("model file map is empty: %w", ErrInvalidArgument)
	}

	filenames := make([]string, 0, len(files))
	for filename := range files {
		if filename == "" || strings.IndexByte(filename, 0) >= 0 {
			return nil, fmt.Errorf("invalid model filename %q: %w", filename, ErrInvalidArgument)
		}
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)

	memory := make([][]byte, len(filenames))
	memorySizes := make([]uint64, len(filenames))
	pinner := new(runtime.Pinner)
	for index, filename := range filenames {
		memory[index] = files[filename]
		memorySizes[index] = uint64(len(memory[index]))
		if len(memory[index]) > 0 {
			pinner.Pin(&memory[index][0])
		}
	}

	handle := bindings.loadTranscriberFromMemoryFiles(
		filenames,
		memory,
		memorySizes,
		uint32(modelArch),
		options,
	)
	if handle < 0 {
		pinner.Unpin()
		return nil, fmt.Errorf(
			"moonshine: load transcriber from memory: %w",
			nativeError(handle, bindings.errorToString(handle)),
		)
	}

	transcriber := &Transcriber{
		bindings: bindings,
		handle:   handle,
		memory:   memory,
		pinner:   pinner,
	}
	runtime.SetFinalizer(transcriber, (*Transcriber).finalize)
	return transcriber, nil
}

// Close releases the native transcriber. It is safe to call Close more than
// once.
func (t *Transcriber) Close() error {
	if t == nil {
		return nil
	}

	t.closeOnce.Do(func() {
		runtime.SetFinalizer(t, nil)
		t.bindings.freeTranscriber(t.handle)
		if t.pinner != nil {
			t.pinner.Unpin()
			t.pinner = nil
		}
		t.memory = nil
	})
	return nil
}

func (t *Transcriber) finalize() {
	_ = t.Close()
}
