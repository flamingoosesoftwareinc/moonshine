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

type textToSpeechBindings interface {
	createTTSFromFiles(language string, filenames []string, options []Option) int32
	createTTSFromMemory(
		language string,
		filenames []string,
		memory [][]byte,
		memorySizes []uint64,
		options []Option,
	) int32
	synthesize(handle int32, text string, options []Option) (Audio, int32, error)
	synthesizePhonemes(handle int32, phonemes string, options []Option) (Audio, int32, error)
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

func (rawTextToSpeechBindings) createTTSFromMemory(
	language string,
	filenames []string,
	memory [][]byte,
	memorySizes []uint64,
	options []Option,
) int32 {
	converted := rawOptions(options)
	return raw.MoonshineCreateTtsSynthesizerFromMemory(
		language,
		filenames,
		uint64(len(filenames)),
		memory,
		memorySizes,
		converted,
		uint64(len(converted)),
		raw.MoonshineHeaderVersion,
	)
}

func (rawTextToSpeechBindings) freeTTS(handle int32) {
	raw.MoonshineFreeTtsSynthesizer(handle)
}

func (rawTextToSpeechBindings) synthesize(handle int32, text string, options []Option) (Audio, int32, error) {
	converted := rawOptions(options)
	output := [][]float32{nil}
	sizes := []uint64{0}
	sampleRates := []int32{0}
	code := raw.MoonshineTextToSpeech(
		handle,
		text,
		converted,
		uint64(len(converted)),
		output,
		sizes,
		sampleRates,
	)
	pointer := unsafe.SliceData(output[0])
	if pointer != nil {
		defer raw.MoonshineFreeBuffer(unsafe.Pointer(pointer))
	}
	if code < 0 {
		return Audio{}, code, nil
	}
	if sizes[0] > uint64(^uint(0)>>1) {
		return Audio{}, code, fmt.Errorf("audio sample count %d exceeds addressable memory", sizes[0])
	}
	if sizes[0] > 0 && pointer == nil {
		return Audio{}, code, fmt.Errorf("native synthesis returned %d samples with a nil buffer", sizes[0])
	}

	samples := append([]float32(nil), unsafe.Slice(pointer, int(sizes[0]))...)
	return Audio{Samples: samples, SampleRate: int(sampleRates[0])}, code, nil
}

func (rawTextToSpeechBindings) synthesizePhonemes(
	handle int32,
	phonemes string,
	options []Option,
) (Audio, int32, error) {
	converted := rawOptions(options)
	output := [][]float32{nil}
	sizes := []uint64{0}
	sampleRates := []int32{0}
	code := raw.MoonshinePhonemesToSpeech(
		handle,
		phonemes,
		converted,
		uint64(len(converted)),
		output,
		sizes,
		sampleRates,
	)
	pointer := unsafe.SliceData(output[0])
	if pointer != nil {
		defer raw.MoonshineFreeBuffer(unsafe.Pointer(pointer))
	}
	if code < 0 {
		return Audio{}, code, nil
	}
	if sizes[0] > uint64(^uint(0)>>1) {
		return Audio{}, code, fmt.Errorf("audio sample count %d exceeds addressable memory", sizes[0])
	}
	if sizes[0] > 0 && pointer == nil {
		return Audio{}, code, fmt.Errorf("native phoneme synthesis returned %d samples with a nil buffer", sizes[0])
	}

	samples := append([]float32(nil), unsafe.Slice(pointer, int(sizes[0]))...)
	return Audio{Samples: samples, SampleRate: int(sampleRates[0])}, code, nil
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
	memory    [][]byte
	pinner    *runtime.Pinner
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
	if err := validateTextToSpeechInput(language, files, options); err != nil {
		return nil, err
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

// NewTextToSpeechFromMemory creates a synthesizer from canonical asset keys
// and buffers. The buffers must not be modified until Close.
func NewTextToSpeechFromMemory(
	language string,
	files map[string][]byte,
	options ...Option,
) (*TextToSpeech, error) {
	return newTextToSpeechFromMemory(rawTextToSpeechBindings{}, language, files, options...)
}

func newTextToSpeechFromMemory(
	bindings textToSpeechBindings,
	language string,
	files map[string][]byte,
	options ...Option,
) (*TextToSpeech, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("TTS file map is empty: %w", ErrInvalidArgument)
	}
	filenames := make([]string, 0, len(files))
	for filename := range files {
		filenames = append(filenames, filename)
	}
	if err := validateTextToSpeechInput(language, filenames, options); err != nil {
		return nil, err
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

	handle := bindings.createTTSFromMemory(
		language, filenames, memory, memorySizes, options,
	)
	if handle < 0 {
		pinner.Unpin()
		return nil, fmt.Errorf(
			"moonshine: create TTS synthesizer from memory for %q: %w",
			language,
			nativeError(handle, bindings.errorToString(handle)),
		)
	}

	synthesizer := &TextToSpeech{
		bindings: bindings,
		handle:   handle,
		memory:   memory,
		pinner:   pinner,
	}
	runtime.SetFinalizer(synthesizer, (*TextToSpeech).finalize)
	return synthesizer, nil
}

func validateTextToSpeechInput(language string, files []string, options []Option) error {
	if language == "" || strings.IndexByte(language, 0) >= 0 {
		return fmt.Errorf("invalid TTS language %q: %w", language, ErrInvalidArgument)
	}
	if err := validateOptions(options); err != nil {
		return err
	}
	for _, filename := range files {
		if filename == "" || strings.IndexByte(filename, 0) >= 0 {
			return fmt.Errorf("invalid TTS filename %q: %w", filename, ErrInvalidArgument)
		}
	}
	return nil
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
		if t.pinner != nil {
			t.pinner.Unpin()
			t.pinner = nil
		}
		t.memory = nil
	})
	return nil
}

// Synthesize converts text into mono float-PCM audio. The returned samples are
// owned by Go and remain valid after later calls and after Close.
func (t *TextToSpeech) Synthesize(text string, options ...Option) (Audio, error) {
	if t == nil {
		return Audio{}, ErrClosed
	}
	if text == "" || strings.IndexByte(text, 0) >= 0 {
		return Audio{}, fmt.Errorf("invalid TTS text: %w", ErrInvalidArgument)
	}
	if err := validateOptions(options); err != nil {
		return Audio{}, err
	}

	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.closed {
		return Audio{}, ErrClosed
	}

	audio, code, err := t.bindings.synthesize(t.handle, text, options)
	runtime.KeepAlive(t)
	if code < 0 {
		return Audio{}, fmt.Errorf(
			"moonshine: synthesize speech: %w",
			nativeError(code, t.bindings.errorToString(code)),
		)
	}
	if err != nil {
		return Audio{}, fmt.Errorf("moonshine: copy synthesized audio: %w", err)
	}
	return audio, nil
}

// SynthesizePhonemes converts an IPA string produced by a matching Phonemizer
// into mono float-PCM audio. The returned samples are owned by Go.
func (t *TextToSpeech) SynthesizePhonemes(phonemes string, options ...Option) (Audio, error) {
	if t == nil {
		return Audio{}, ErrClosed
	}
	if phonemes == "" || strings.IndexByte(phonemes, 0) >= 0 {
		return Audio{}, fmt.Errorf("invalid TTS phonemes: %w", ErrInvalidArgument)
	}
	if err := validateOptions(options); err != nil {
		return Audio{}, err
	}

	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.closed {
		return Audio{}, ErrClosed
	}

	audio, code, err := t.bindings.synthesizePhonemes(t.handle, phonemes, options)
	runtime.KeepAlive(t)
	if code < 0 {
		return Audio{}, fmt.Errorf(
			"moonshine: synthesize phonemes: %w",
			nativeError(code, t.bindings.errorToString(code)),
		)
	}
	if err != nil {
		return Audio{}, fmt.Errorf("moonshine: copy phoneme synthesis audio: %w", err)
	}
	return audio, nil
}

func (t *TextToSpeech) finalize() {
	_ = t.Close()
}
