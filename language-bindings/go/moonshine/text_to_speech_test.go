package moonshine

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const rawErrorInvalidHandle int32 = -2

type fakeTextToSpeechBindings struct {
	handle            int32
	errorMessage      string
	languages         []string
	filenames         [][]string
	memory            [][][]byte
	memorySizes       [][]uint64
	options           [][]Option
	freed             []int32
	audio             Audio
	synthesizeCode    int32
	synthesizeErr     error
	texts             []string
	synthesizeOptions [][]Option
	phonemes          []string
	phonemeOptions    [][]Option
	phonemeCode       int32
	phonemeErr        error
	clip              SpeechClip
	clips             []SpeechClip
	clipCode          int32
	clipErr           error
	clipAudio         [][]float32
	clipSampleRates   []int
	clipOptions       [][]Option
}

func (f *fakeTextToSpeechBindings) synthesize(_ int32, text string, options []Option) (Audio, int32, error) {
	f.texts = append(f.texts, text)
	f.synthesizeOptions = append(f.synthesizeOptions, append([]Option(nil), options...))
	return f.audio, f.synthesizeCode, f.synthesizeErr
}

func (f *fakeTextToSpeechBindings) synthesizePhonemes(
	_ int32, phonemes string, options []Option,
) (Audio, int32, error) {
	f.phonemes = append(f.phonemes, phonemes)
	f.phonemeOptions = append(f.phonemeOptions, append([]Option(nil), options...))
	return f.audio, f.phonemeCode, f.phonemeErr
}

func (f *fakeTextToSpeechBindings) extractSpeechClip(
	_ int32, audio []float32, sampleRate int, options []Option,
) (SpeechClip, int32, error) {
	f.clipAudio = append(f.clipAudio, append([]float32(nil), audio...))
	f.clipSampleRates = append(f.clipSampleRates, sampleRate)
	f.clipOptions = append(f.clipOptions, append([]Option(nil), options...))
	if len(f.clips) > 0 {
		index := min(len(f.clipAudio)-1, len(f.clips)-1)
		return f.clips[index], f.clipCode, f.clipErr
	}
	return f.clip, f.clipCode, f.clipErr
}

func (f *fakeTextToSpeechBindings) createTTSFromFiles(language string, filenames []string, options []Option) int32 {
	f.languages = append(f.languages, language)
	f.filenames = append(f.filenames, append([]string(nil), filenames...))
	f.options = append(f.options, append([]Option(nil), options...))
	return f.handle
}

func (f *fakeTextToSpeechBindings) createTTSFromMemory(
	language string,
	filenames []string,
	memory [][]byte,
	memorySizes []uint64,
	options []Option,
) int32 {
	f.languages = append(f.languages, language)
	f.filenames = append(f.filenames, append([]string(nil), filenames...))
	f.memory = append(f.memory, append([][]byte(nil), memory...))
	f.memorySizes = append(f.memorySizes, append([]uint64(nil), memorySizes...))
	f.options = append(f.options, append([]Option(nil), options...))
	return f.handle
}

func (f *fakeTextToSpeechBindings) freeTTS(handle int32) {
	f.freed = append(f.freed, handle)
}

func (f *fakeTextToSpeechBindings) errorToString(int32) string {
	return f.errorMessage
}

func TestNewTextToSpeechFromFilesCreatesAndClosesNativeHandle(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{handle: 42}
	files := []string{"kokoro/model.ort", "kokoro/config.json"}
	options := []Option{{Name: "g2p_root", Value: "/models/tts"}}

	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", files, options...)
	require.NoError(t, err)
	assert.Equal(t, []string{"en_us"}, bindings.languages)
	assert.Equal(t, [][]string{files}, bindings.filenames)
	assert.Equal(t, [][]Option{options}, bindings.options)

	require.NoError(t, synthesizer.Close())
	require.NoError(t, synthesizer.Close())
	assert.Equal(t, []int32{42}, bindings.freed)
}

func TestNewTextToSpeechFromFilesMapsNativeErrorToSentinel(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{
		handle:       rawErrorInvalidArgument,
		errorMessage: "Invalid argument",
	}

	synthesizer, err := newTextToSpeechFromFiles(bindings, "xx_invalid", nil)

	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Nil(t, synthesizer)
	assert.Empty(t, bindings.freed)
}

func TestNewTextToSpeechFromMemorySortsPinsAndReleasesFiles(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{handle: 42}
	files := map[string][]byte{
		"kokoro/model.ort":   {3},
		"kokoro/config.json": {1, 2},
	}
	options := []Option{{Name: "voice", Value: "af_heart"}}

	synthesizer, err := newTextToSpeechFromMemory(bindings, "en_us", files, options...)

	require.NoError(t, err)
	assert.Equal(t, []string{"en_us"}, bindings.languages)
	assert.Equal(t, [][]string{{"kokoro/config.json", "kokoro/model.ort"}}, bindings.filenames)
	assert.Equal(t, [][][]byte{{{1, 2}, {3}}}, bindings.memory)
	assert.Equal(t, [][]uint64{{2, 1}}, bindings.memorySizes)
	assert.Equal(t, [][]Option{options}, bindings.options)
	assert.NotNil(t, synthesizer.pinner)
	require.NoError(t, synthesizer.Close())
	assert.Nil(t, synthesizer.pinner)
	assert.Nil(t, synthesizer.memory)
	assert.Equal(t, []int32{42}, bindings.freed)
}

func TestNewTextToSpeechFromMemoryRejectsInvalidFiles(t *testing.T) {
	tests := []map[string][]byte{
		nil,
		{},
		{"": {1}},
		{"bad\x00name": {1}},
	}
	for _, files := range tests {
		bindings := &fakeTextToSpeechBindings{handle: 42}
		synthesizer, err := newTextToSpeechFromMemory(bindings, "en_us", files)
		require.ErrorIs(t, err, ErrInvalidArgument)
		assert.Nil(t, synthesizer)
		assert.Empty(t, bindings.languages)
	}
}

func TestNewTextToSpeechFromMemoryReleasesPinsAfterNativeError(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{
		handle: rawErrorInvalidArgument, errorMessage: "Invalid argument",
	}

	synthesizer, err := newTextToSpeechFromMemory(
		bindings, "en_us", map[string][]byte{"kokoro/model.ort": {1}},
	)

	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Nil(t, synthesizer)
	assert.Empty(t, bindings.freed)
}

func TestNewTextToSpeechFromFilesRejectsInvalidInputBeforeNativeCall(t *testing.T) {
	tests := []struct {
		name     string
		language string
		files    []string
		options  []Option
	}{
		{name: "empty language"},
		{name: "NUL language", language: "en\x00us"},
		{name: "empty filename", language: "en_us", files: []string{""}},
		{name: "NUL filename", language: "en_us", files: []string{"bad\x00name"}},
		{name: "invalid option", language: "en_us", options: []Option{{Name: ""}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bindings := &fakeTextToSpeechBindings{handle: 42}

			synthesizer, err := newTextToSpeechFromFiles(
				bindings,
				test.language,
				test.files,
				test.options...,
			)

			require.ErrorIs(t, err, ErrInvalidArgument)
			assert.Nil(t, synthesizer)
			assert.Empty(t, bindings.languages)
		})
	}
}

func TestNilTextToSpeechClose(t *testing.T) {
	var synthesizer *TextToSpeech
	require.NoError(t, synthesizer.Close())
}

func TestTextToSpeechCloseIsConcurrentAndIdempotent(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{handle: 42}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)

	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_ = synthesizer.Close()
		}()
	}
	wait.Wait()

	assert.Equal(t, []int32{42}, bindings.freed)
}

func TestTextToSpeechSynthesizeReturnsGoOwnedAudio(t *testing.T) {
	want := Audio{Samples: []float32{0.25, -0.5}, SampleRate: 24000}
	bindings := &fakeTextToSpeechBindings{handle: 42, audio: want}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, synthesizer.Close()) })
	options := []Option{{Name: "speed", Value: "1.5"}}

	got, err := synthesizer.Synthesize("Hello world!", options...)

	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, []string{"Hello world!"}, bindings.texts)
	assert.Equal(t, [][]Option{options}, bindings.synthesizeOptions)
}

func TestTextToSpeechSynthesizeMapsNativeErrorToSentinel(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{
		handle:         42,
		synthesizeCode: rawErrorInvalidHandle,
		errorMessage:   "Invalid handle",
	}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, synthesizer.Close()) })

	_, err = synthesizer.Synthesize("Hello")

	require.ErrorIs(t, err, ErrInvalidHandle)
}

func TestTextToSpeechSynthesizeRejectsInvalidInput(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{handle: 42}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, synthesizer.Close()) })

	_, err = synthesizer.Synthesize("")
	require.ErrorIs(t, err, ErrInvalidArgument)
	_, err = synthesizer.Synthesize("bad\x00text")
	require.ErrorIs(t, err, ErrInvalidArgument)
	_, err = synthesizer.Synthesize("hello", Option{})
	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Empty(t, bindings.texts)
}

func TestTextToSpeechSynthesizeAfterClose(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{handle: 42}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	require.NoError(t, synthesizer.Close())

	_, err = synthesizer.Synthesize("Hello")

	require.ErrorIs(t, err, ErrClosed)
}

func TestTextToSpeechSynthesizePhonemesReturnsGoOwnedAudio(t *testing.T) {
	want := Audio{Samples: []float32{0.25, -0.5}, SampleRate: 24000}
	bindings := &fakeTextToSpeechBindings{handle: 42, audio: want}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, synthesizer.Close()) })
	options := []Option{{Name: "speed", Value: "1.25"}}

	got, err := synthesizer.SynthesizePhonemes("həˈloʊ", options...)

	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, []string{"həˈloʊ"}, bindings.phonemes)
	assert.Equal(t, [][]Option{options}, bindings.phonemeOptions)
}

func TestTextToSpeechSynthesizePhonemesMapsNativeErrorToSentinel(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{
		handle: 42, phonemeCode: rawErrorInvalidHandle, errorMessage: "Invalid handle",
	}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, synthesizer.Close()) })

	_, err = synthesizer.SynthesizePhonemes("həˈloʊ")

	require.ErrorIs(t, err, ErrInvalidHandle)
}

func TestTextToSpeechSynthesizePhonemesRejectsInvalidInput(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{handle: 42}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, synthesizer.Close()) })

	_, err = synthesizer.SynthesizePhonemes("")
	require.ErrorIs(t, err, ErrInvalidArgument)
	_, err = synthesizer.SynthesizePhonemes("bad\x00phonemes")
	require.ErrorIs(t, err, ErrInvalidArgument)
	_, err = synthesizer.SynthesizePhonemes("həˈloʊ", Option{})
	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Empty(t, bindings.phonemes)
}

func TestTextToSpeechSynthesizePhonemesAfterClose(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{handle: 42}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	require.NoError(t, synthesizer.Close())

	_, err = synthesizer.SynthesizePhonemes("həˈloʊ")
	require.ErrorIs(t, err, ErrClosed)

	var nilSynthesizer *TextToSpeech
	_, err = nilSynthesizer.SynthesizePhonemes("həˈloʊ")
	require.ErrorIs(t, err, ErrClosed)
}

func TestTextToSpeechExtractSpeechClipForwardsAndReturnsOwnedResult(t *testing.T) {
	want := SpeechClip{
		Audio: Audio{Samples: []float32{0.1, 0.2}, SampleRate: 16000},
		Start: 1.25, Duration: 2.5, Complete: true, Transcript: "hello",
	}
	bindings := &fakeTextToSpeechBindings{handle: 42, clip: want}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, synthesizer.Close()) })
	options := []Option{{Name: "minimum_speech_seconds", Value: "1"}}

	got, err := synthesizer.ExtractSpeechClip([]float32{0.25, -0.5}, 24000, options...)

	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, [][]float32{{0.25, -0.5}}, bindings.clipAudio)
	assert.Equal(t, []int{24000}, bindings.clipSampleRates)
	assert.Equal(t, [][]Option{options}, bindings.clipOptions)
}

func TestTextToSpeechExtractSpeechClipReturnsIncompleteProgress(t *testing.T) {
	want := SpeechClip{Start: 0.5, Duration: 0.75}
	bindings := &fakeTextToSpeechBindings{handle: 42, clip: want}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, synthesizer.Close()) })

	got, err := synthesizer.ExtractSpeechClip([]float32{0}, 16000)

	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.False(t, got.Complete)
	assert.Empty(t, got.Audio.Samples)
}

func TestTextToSpeechExtractSpeechClipMapsErrors(t *testing.T) {
	t.Run("native sentinel", func(t *testing.T) {
		bindings := &fakeTextToSpeechBindings{
			handle: 42, clipCode: rawErrorInvalidHandle, errorMessage: "Invalid handle",
		}
		synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, synthesizer.Close()) })
		_, err = synthesizer.ExtractSpeechClip([]float32{0}, 16000)
		require.ErrorIs(t, err, ErrInvalidHandle)
	})
	t.Run("copy failure", func(t *testing.T) {
		copyErr := errors.New("copy failed")
		bindings := &fakeTextToSpeechBindings{handle: 42, clipErr: copyErr}
		synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, synthesizer.Close()) })
		_, err = synthesizer.ExtractSpeechClip([]float32{0}, 16000)
		require.ErrorIs(t, err, copyErr)
	})
}

func TestTextToSpeechExtractSpeechClipRejectsInvalidStateAndInput(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{handle: 42}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)

	_, err = synthesizer.ExtractSpeechClip(nil, 16000)
	require.ErrorIs(t, err, ErrInvalidArgument)
	_, err = synthesizer.ExtractSpeechClip([]float32{0}, 0)
	require.ErrorIs(t, err, ErrInvalidArgument)
	_, err = synthesizer.ExtractSpeechClip([]float32{0}, 16000, Option{})
	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Empty(t, bindings.clipAudio)

	require.NoError(t, synthesizer.Close())
	_, err = synthesizer.ExtractSpeechClip([]float32{0}, 16000)
	require.ErrorIs(t, err, ErrClosed)
	var nilSynthesizer *TextToSpeech
	_, err = nilSynthesizer.ExtractSpeechClip([]float32{0}, 16000)
	require.ErrorIs(t, err, ErrClosed)
}
