package moonshine

import (
	"testing"
	"unsafe"

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
	phonemes     []byte
	phonemeCount uint64
	phonemeCode  int32
	phonemeCalls []phonemeCall
	freedBuffers []unsafe.Pointer
}

type phonemeCall struct {
	handle  int32
	text    string
	options []Option
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
func (f *fakePhonemizerBindings) textToPhonemes(
	handle int32, text string, options []Option,
) (unsafe.Pointer, uint64, int32) {
	f.phonemeCalls = append(f.phonemeCalls, phonemeCall{
		handle: handle, text: text, options: append([]Option(nil), options...),
	})
	if len(f.phonemes) == 0 {
		return nil, f.phonemeCount, f.phonemeCode
	}
	return unsafe.Pointer(&f.phonemes[0]), f.phonemeCount, f.phonemeCode
}
func (f *fakePhonemizerBindings) freeBuffer(pointer unsafe.Pointer) {
	f.freedBuffers = append(f.freedBuffers, pointer)
}
func (f *fakePhonemizerBindings) errorToString(int32) string { return f.errorMessage }

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

func TestPhonemizerPhonemesCopiesAndReleasesNativeResult(t *testing.T) {
	bindings := &fakePhonemizerBindings{
		handle: 42, phonemes: []byte("həˈloʊ"), phonemeCount: uint64(len("həˈloʊ")),
	}
	phonemizer, err := newPhonemizerFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, phonemizer.Close()) })
	options := []Option{{Name: "mode", Value: "test"}}

	got, err := phonemizer.Phonemes("hello", options...)

	require.NoError(t, err)
	assert.Equal(t, "həˈloʊ", got)
	assert.Equal(t, []phonemeCall{{handle: 42, text: "hello", options: options}}, bindings.phonemeCalls)
	assert.Len(t, bindings.freedBuffers, 1)
	bindings.phonemes[0] = 'x'
	assert.Equal(t, "həˈloʊ", got)
}

func TestPhonemizerPhonemesMapsNativeErrorAndReleasesResult(t *testing.T) {
	bindings := &fakePhonemizerBindings{
		handle: 42, phonemes: []byte("partial"), phonemeCount: 7,
		phonemeCode: rawErrorInvalidArgument, errorMessage: "Invalid argument",
	}
	phonemizer, err := newPhonemizerFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, phonemizer.Close()) })

	got, err := phonemizer.Phonemes("hello")

	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Empty(t, got)
	assert.Len(t, bindings.freedBuffers, 1)
}

func TestPhonemizerPhonemesRejectsInvalidNativeOutput(t *testing.T) {
	bindings := &fakePhonemizerBindings{handle: 42, phonemeCount: 1}
	phonemizer, err := newPhonemizerFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, phonemizer.Close()) })

	got, err := phonemizer.Phonemes("hello")

	require.ErrorIs(t, err, ErrInvalidNativeOutput)
	assert.Empty(t, got)
}

func TestPhonemizerPhonemesRejectsInvalidInput(t *testing.T) {
	bindings := &fakePhonemizerBindings{handle: 42}
	phonemizer, err := newPhonemizerFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, phonemizer.Close()) })

	_, err = phonemizer.Phonemes("bad\x00text")
	require.ErrorIs(t, err, ErrInvalidArgument)
	_, err = phonemizer.Phonemes("hello", Option{Name: ""})
	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Empty(t, bindings.phonemeCalls)
}

func TestPhonemizerPhonemesAfterClose(t *testing.T) {
	bindings := &fakePhonemizerBindings{handle: 42}
	phonemizer, err := newPhonemizerFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	require.NoError(t, phonemizer.Close())

	_, err = phonemizer.Phonemes("hello")
	require.ErrorIs(t, err, ErrClosed)

	var nilPhonemizer *Phonemizer
	_, err = nilPhonemizer.Phonemes("hello")
	require.ErrorIs(t, err, ErrClosed)
}
