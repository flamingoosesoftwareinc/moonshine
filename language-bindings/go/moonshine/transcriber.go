package moonshine

import (
	"fmt"
	"runtime"
	"sync"

	"github.com/moonshine-ai/moonshine/language-bindings/go/raw"
)

type transcriberBindings interface {
	loadTranscriberFromFiles(path string, modelArch uint32) int32
	freeTranscriber(handle int32)
}

type rawTranscriberBindings struct{}

func (rawTranscriberBindings) loadTranscriberFromFiles(path string, modelArch uint32) int32 {
	return raw.MoonshineLoadTranscriberFromFiles(
		path,
		modelArch,
		nil,
		0,
		raw.MoonshineHeaderVersion,
	)
}

func (rawTranscriberBindings) freeTranscriber(handle int32) {
	raw.MoonshineFreeTranscriber(handle)
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
func NewTranscriber(modelPath string, modelArch ModelArch) (*Transcriber, error) {
	return newTranscriber(rawTranscriberBindings{}, modelPath, modelArch)
}

func newTranscriber(bindings transcriberBindings, modelPath string, modelArch ModelArch) (*Transcriber, error) {
	handle := bindings.loadTranscriberFromFiles(modelPath, uint32(modelArch))
	if handle < 0 {
		return nil, fmt.Errorf("moonshine: load transcriber from %q: error code %d", modelPath, handle)
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
