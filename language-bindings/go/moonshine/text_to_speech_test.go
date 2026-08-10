package moonshine

import (
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
	options           [][]Option
	freed             []int32
	audio             Audio
	synthesizeCode    int32
	synthesizeErr     error
	texts             []string
	synthesizeOptions [][]Option
}

func (f *fakeTextToSpeechBindings) synthesize(_ int32, text string, options []Option) (Audio, int32, error) {
	f.texts = append(f.texts, text)
	f.synthesizeOptions = append(f.synthesizeOptions, append([]Option(nil), options...))
	return f.audio, f.synthesizeCode, f.synthesizeErr
}

func (f *fakeTextToSpeechBindings) createTTSFromFiles(language string, filenames []string, options []Option) int32 {
	f.languages = append(f.languages, language)
	f.filenames = append(f.filenames, append([]string(nil), filenames...))
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
