package moonshine

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTextToSpeechBindings struct {
	handle       int32
	errorMessage string
	languages    []string
	filenames    [][]string
	options      [][]Option
	freed        []int32
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
