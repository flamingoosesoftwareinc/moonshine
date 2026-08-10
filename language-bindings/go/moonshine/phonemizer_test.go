package moonshine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePhonemizerBindings struct {
	handle       int32
	errorMessage string
	languages    []string
	filenames    [][]string
	memory       [][][]byte
	sizes        [][]uint64
	options      [][]Option
	freed        []int32
}

func (f *fakePhonemizerBindings) createPhonemizerFromFiles(
	language string, filenames []string, options []Option,
) int32 {
	f.languages = append(f.languages, language)
	f.filenames = append(f.filenames, append([]string(nil), filenames...))
	f.options = append(f.options, append([]Option(nil), options...))
	return f.handle
}

func (f *fakePhonemizerBindings) createPhonemizerFromMemory(
	language string,
	filenames []string,
	memory [][]byte,
	sizes []uint64,
	options []Option,
) int32 {
	f.languages = append(f.languages, language)
	f.filenames = append(f.filenames, append([]string(nil), filenames...))
	f.memory = append(f.memory, append([][]byte(nil), memory...))
	f.sizes = append(f.sizes, append([]uint64(nil), sizes...))
	f.options = append(f.options, append([]Option(nil), options...))
	return f.handle
}

func (f *fakePhonemizerBindings) freePhonemizer(handle int32) { f.freed = append(f.freed, handle) }
func (f *fakePhonemizerBindings) errorToString(int32) string  { return f.errorMessage }

func TestNewPhonemizerFromFilesCreatesAndClosesHandle(t *testing.T) {
	bindings := &fakePhonemizerBindings{handle: 42}
	files := []string{"en_us/dict_filtered_heteronyms.tsv", "en_us/g2p-config.json"}
	options := []Option{{Name: "g2p_root", Value: "/models/tts"}}

	phonemizer, err := newPhonemizerFromFiles(bindings, "en_us", files, options...)

	require.NoError(t, err)
	assert.Equal(t, "en_us", phonemizer.Language())
	assert.Equal(t, []string{"en_us"}, bindings.languages)
	assert.Equal(t, [][]string{files}, bindings.filenames)
	assert.Equal(t, [][]Option{options}, bindings.options)
	require.NoError(t, phonemizer.Close())
	require.NoError(t, phonemizer.Close())
	assert.Equal(t, []int32{42}, bindings.freed)
}

func TestNewPhonemizerFromMemorySortsPinsAndReleasesFiles(t *testing.T) {
	bindings := &fakePhonemizerBindings{handle: 42}
	files := map[string][]byte{"en_us/oov/model.ort": {3}, "en_us/g2p-config.json": {1, 2}}

	phonemizer, err := newPhonemizerFromMemory(bindings, "en_us", files)

	require.NoError(t, err)
	assert.Equal(t, [][]string{{"en_us/g2p-config.json", "en_us/oov/model.ort"}}, bindings.filenames)
	assert.Equal(t, [][][]byte{{{1, 2}, {3}}}, bindings.memory)
	assert.Equal(t, [][]uint64{{2, 1}}, bindings.sizes)
	assert.NotNil(t, phonemizer.pinner)
	require.NoError(t, phonemizer.Close())
	assert.Nil(t, phonemizer.pinner)
	assert.Nil(t, phonemizer.memory)
}

func TestPhonemizerConstructorsMapNativeErrors(t *testing.T) {
	bindings := &fakePhonemizerBindings{
		handle:       rawErrorInvalidArgument,
		errorMessage: "Invalid argument",
	}

	phonemizer, err := newPhonemizerFromFiles(bindings, "xx", nil)

	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Nil(t, phonemizer)
	assert.Empty(t, bindings.freed)
}

func TestNewPhonemizerFromMemoryReleasesPinsAfterNativeError(t *testing.T) {
	bindings := &fakePhonemizerBindings{
		handle:       rawErrorInvalidArgument,
		errorMessage: "Invalid argument",
	}

	phonemizer, err := newPhonemizerFromMemory(
		bindings,
		"en_us",
		map[string][]byte{"en_us/g2p-config.json": {1}},
	)

	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Nil(t, phonemizer)
	assert.Empty(t, bindings.freed)
}

func TestPhonemizerConstructorsRejectInvalidInput(t *testing.T) {
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
			bindings := &fakePhonemizerBindings{handle: 42}
			phonemizer, err := newPhonemizerFromFiles(
				bindings, test.language, test.files, test.options...,
			)
			require.ErrorIs(t, err, ErrInvalidArgument)
			assert.Nil(t, phonemizer)
			assert.Empty(t, bindings.languages)
		})
	}
}

func TestNewPhonemizerFromMemoryRejectsEmptyMap(t *testing.T) {
	bindings := &fakePhonemizerBindings{handle: 42}
	phonemizer, err := newPhonemizerFromMemory(bindings, "en_us", nil)
	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Nil(t, phonemizer)
}

func TestNilPhonemizerCloseAndLanguage(t *testing.T) {
	var phonemizer *Phonemizer
	require.NoError(t, phonemizer.Close())
	assert.Empty(t, phonemizer.Language())
}
