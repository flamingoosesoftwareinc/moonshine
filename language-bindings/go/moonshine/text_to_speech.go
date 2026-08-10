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
	synthesize(handle int32, text string, options []Option) (nativeAudio, int32, error)
	synthesizePhonemes(handle int32, phonemes string, options []Option) (nativeAudio, int32, error)
	extractSpeechClip(
		handle int32, audio []float32, sampleRate int, options []Option,
	) (nativeSpeechClip, int32, error)
	freeBuffer(pointer unsafe.Pointer)
	freeTTS(handle int32)
	errorToString(code int32) string
}

type rawTextToSpeechBindings struct{}

type nativeAudio struct {
	pointer    *float32
	length     uint64
	sampleRate int32
}

type nativeSpeechClip struct {
	audio       *float32
	audioLength uint64
	start       float32
	duration    float32
	complete    bool
	transcript  *byte
}

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

func (rawTextToSpeechBindings) synthesize(handle int32, text string, options []Option) (nativeAudio, int32, error) {
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
	return nativeAudio{
		pointer: unsafe.SliceData(output[0]), length: sizes[0], sampleRate: sampleRates[0],
	}, code, nil
}

func (rawTextToSpeechBindings) synthesizePhonemes(
	handle int32,
	phonemes string,
	options []Option,
) (nativeAudio, int32, error) {
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
	return nativeAudio{
		pointer: unsafe.SliceData(output[0]), length: sizes[0], sampleRate: sampleRates[0],
	}, code, nil
}

func (rawTextToSpeechBindings) extractSpeechClip(
	handle int32,
	audio []float32,
	sampleRate int,
	options []Option,
) (nativeSpeechClip, int32, error) {
	converted := rawOptions(options)
	output := []raw.MoonshineSpeechClipT{{}}
	code := raw.MoonshineExtractSpeechClip(
		audio, uint64(len(audio)), int32(sampleRate), handle,
		converted, uint64(len(converted)), output,
	)
	output[0].Deref()
	return nativeSpeechClip{
		audio:       unsafe.SliceData(output[0].AudioData),
		audioLength: output[0].AudioLength,
		start:       output[0].StartTime,
		duration:    output[0].SpeechDuration,
		complete:    output[0].IsComplete != 0,
		transcript:  unsafe.SliceData(output[0].Transcript),
	}, code, nil
}

func (rawTextToSpeechBindings) freeBuffer(pointer unsafe.Pointer) {
	raw.MoonshineFreeBuffer(pointer)
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

// SpeechClip is a Go-owned voice-cloning reference window extracted from a
// recording. Audio is always mono 16 kHz PCM when Complete is true.
type SpeechClip struct {
	Audio      Audio
	Start      float32
	Duration   float32
	Complete   bool
	Transcript string
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

	native, code, err := t.bindings.synthesize(t.handle, text, options)
	if native.pointer != nil {
		defer t.bindings.freeBuffer(unsafe.Pointer(native.pointer))
	}
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
	audio, err := copyNativeAudio(native)
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

	native, code, err := t.bindings.synthesizePhonemes(t.handle, phonemes, options)
	if native.pointer != nil {
		defer t.bindings.freeBuffer(unsafe.Pointer(native.pointer))
	}
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
	audio, err := copyNativeAudio(native)
	if err != nil {
		return Audio{}, fmt.Errorf("moonshine: copy phoneme synthesis audio: %w", err)
	}
	return audio, nil
}

// ExtractSpeechClip finds the strongest short speech window in a recording.
// An incomplete result reports progress without returning clip samples.
func (t *TextToSpeech) ExtractSpeechClip(
	audio []float32,
	sampleRate int,
	options ...Option,
) (SpeechClip, error) {
	if t == nil {
		return SpeechClip{}, ErrClosed
	}
	if len(audio) == 0 || sampleRate <= 0 {
		return SpeechClip{}, fmt.Errorf("invalid speech clip audio: %w", ErrInvalidArgument)
	}
	if err := validateOptions(options); err != nil {
		return SpeechClip{}, err
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.closed {
		return SpeechClip{}, ErrClosed
	}
	native, code, err := t.bindings.extractSpeechClip(t.handle, audio, sampleRate, options)
	if native.audio != nil {
		defer t.bindings.freeBuffer(unsafe.Pointer(native.audio))
	}
	if native.transcript != nil {
		defer t.bindings.freeBuffer(unsafe.Pointer(native.transcript))
	}
	runtime.KeepAlive(t)
	if code < 0 {
		return SpeechClip{}, fmt.Errorf(
			"moonshine: extract speech clip: %w",
			nativeError(code, t.bindings.errorToString(code)),
		)
	}
	if err != nil {
		return SpeechClip{}, fmt.Errorf("moonshine: copy speech clip: %w", err)
	}
	clip, err := copyNativeSpeechClip(native)
	if err != nil {
		return SpeechClip{}, fmt.Errorf("moonshine: copy speech clip: %w", err)
	}
	return clip, nil
}

func copyNativeAudio(native nativeAudio) (Audio, error) {
	if native.length > uint64(^uint(0)>>1) {
		return Audio{}, fmt.Errorf("audio sample count %d exceeds addressable memory: %w", native.length, ErrInvalidNativeOutput)
	}
	if native.length > 0 && native.pointer == nil {
		return Audio{}, fmt.Errorf("native audio returned %d samples with a nil buffer: %w", native.length, ErrInvalidNativeOutput)
	}
	if native.sampleRate <= 0 {
		return Audio{}, fmt.Errorf("native audio returned invalid sample rate %d: %w", native.sampleRate, ErrInvalidNativeOutput)
	}
	return Audio{
		Samples:    append([]float32(nil), unsafe.Slice(native.pointer, int(native.length))...),
		SampleRate: int(native.sampleRate),
	}, nil
}

func copyNativeSpeechClip(native nativeSpeechClip) (SpeechClip, error) {
	if native.audioLength > uint64(^uint(0)>>1) {
		return SpeechClip{}, fmt.Errorf("speech clip sample count %d exceeds addressable memory: %w", native.audioLength, ErrInvalidNativeOutput)
	}
	if native.audioLength > 0 && native.audio == nil {
		return SpeechClip{}, fmt.Errorf("native speech clip returned %d samples with a nil buffer: %w", native.audioLength, ErrInvalidNativeOutput)
	}
	sampleRate := 0
	if native.complete || native.audioLength > 0 {
		sampleRate = 16000
	}
	return SpeechClip{
		Audio: Audio{
			Samples:    append([]float32(nil), unsafe.Slice(native.audio, int(native.audioLength))...),
			SampleRate: sampleRate,
		},
		Start:      native.start,
		Duration:   native.duration,
		Complete:   native.complete,
		Transcript: copyCString(native.transcript),
	}, nil
}

func (t *TextToSpeech) finalize() {
	_ = t.Close()
}
