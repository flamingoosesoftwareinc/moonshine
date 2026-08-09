package moonshine

import (
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/moonshine-ai/moonshine/language-bindings/go/raw"
)

type textToSpeechBindings interface {
	createTTSFromFiles(language string, filenames []string, options []Option) int32
	freeTTS(handle int32)
	errorToString(code int32) string
}

type rawTextToSpeechBindings struct{}

func (rawTextToSpeechBindings) createTTSFromFiles(language string, filenames []string, options []Option) int32 {
	converted := rawOptions(options)
	return raw.MoonshineCreateTtsSynthesizerFromFiles(
		language,
		filenames,
		uint64(len(filenames)),
		converted,
		uint64(len(converted)),
		raw.MoonshineHeaderVersion,
	)
}

func (rawTextToSpeechBindings) freeTTS(handle int32) {
	raw.MoonshineFreeTtsSynthesizer(handle)
}

func (rawTextToSpeechBindings) errorToString(code int32) string {
	return copyCString(raw.MoonshineErrorToString(code))
}

// TextToSpeech owns a native Moonshine text-to-speech synthesizer.
//
// Call Close when the synthesizer is no longer needed. A finalizer releases
// abandoned synthesizers as a fallback, but callers should not rely on its
// timing. A TextToSpeech must not be copied after first use.
type TextToSpeech struct {
	bindings  textToSpeechBindings
	handle    int32
	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once
}

// NewTextToSpeechFromFiles creates a synthesizer from canonical asset keys.
// Assets may be resolved relative to the g2p_root option.
func NewTextToSpeechFromFiles(language string, files []string, options ...Option) (*TextToSpeech, error) {
	return newTextToSpeechFromFiles(rawTextToSpeechBindings{}, language, files, options...)
}

func newTextToSpeechFromFiles(
	bindings textToSpeechBindings,
	language string,
	files []string,
	options ...Option,
) (*TextToSpeech, error) {
	if language == "" || strings.IndexByte(language, 0) >= 0 {
		return nil, fmt.Errorf("invalid TTS language %q: %w", language, ErrInvalidArgument)
	}
	if err := validateOptions(options); err != nil {
		return nil, err
	}
	for _, filename := range files {
		if filename == "" || strings.IndexByte(filename, 0) >= 0 {
			return nil, fmt.Errorf("invalid TTS filename %q: %w", filename, ErrInvalidArgument)
		}
	}

	filenames := append([]string(nil), files...)
	handle := bindings.createTTSFromFiles(language, filenames, options)
	if handle < 0 {
		return nil, fmt.Errorf(
			"moonshine: create TTS synthesizer for %q: %w",
			language,
			nativeError(handle, bindings.errorToString(handle)),
		)
	}

	synthesizer := &TextToSpeech{bindings: bindings, handle: handle}
	runtime.SetFinalizer(synthesizer, (*TextToSpeech).finalize)
	return synthesizer, nil
}

// Close releases the native synthesizer. It is safe to call Close more than
// once.
func (t *TextToSpeech) Close() error {
	if t == nil {
		return nil
	}

	t.closeOnce.Do(func() {
		t.mu.Lock()
		defer t.mu.Unlock()

		runtime.SetFinalizer(t, nil)
		t.closed = true
		t.bindings.freeTTS(t.handle)
	})
	return nil
}

func (t *TextToSpeech) finalize() {
	_ = t.Close()
}
