package moonshine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTranscriberBindings struct {
	handle       int32
	errorMessage string
	paths        []string
	arches       []uint32
	options      [][]Option
	freed        []int32
}

func (f *fakeTranscriberBindings) loadTranscriberFromFiles(path string, modelArch uint32, options []Option) int32 {
	f.paths = append(f.paths, path)
	f.arches = append(f.arches, modelArch)
	f.options = append(f.options, append([]Option(nil), options...))
	return f.handle
}

func (f *fakeTranscriberBindings) freeTranscriber(handle int32) {
	f.freed = append(f.freed, handle)
}

func (f *fakeTranscriberBindings) errorToString(int32) string {
	return f.errorMessage
}

func TestNewTranscriberLoadsAndClosesNativeHandle(t *testing.T) {
	bindings := &fakeTranscriberBindings{handle: 42}

	options := []Option{{Name: "word_timestamps", Value: "true"}}
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny, options...)
	require.NoError(t, err)
	assert.Equal(t, []string{"/models/tiny-en"}, bindings.paths)
	assert.Equal(t, []uint32{uint32(ModelArchTiny)}, bindings.arches)
	assert.Equal(t, [][]Option{options}, bindings.options)

	transcriber.Close()
	transcriber.Close()

	assert.Equal(t, []int32{42}, bindings.freed)
}

func TestNewTranscriberReturnsLoadError(t *testing.T) {
	bindings := &fakeTranscriberBindings{
		handle:       rawErrorInvalidArgument,
		errorMessage: "Invalid argument",
	}

	transcriber, err := newTranscriber(bindings, "/missing", ModelArchBase)
	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Nil(t, transcriber)
	assert.Empty(t, bindings.freed)
}

func TestNilTranscriberClose(t *testing.T) {
	var transcriber *Transcriber
	transcriber.Close()
}

func TestNewTranscriberRejectsInvalidInputBeforeNativeCall(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		options []Option
	}{
		{name: "NUL path", path: "/models\x00/tiny"},
		{name: "invalid option", path: "/models", options: []Option{{Value: "true"}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bindings := &fakeTranscriberBindings{handle: 42}

			transcriber, err := newTranscriber(bindings, test.path, ModelArchTiny, test.options...)

			require.ErrorIs(t, err, ErrInvalidArgument)
			assert.Nil(t, transcriber)
			assert.Empty(t, bindings.paths)
			assert.Empty(t, bindings.freed)
		})
	}
}

const rawErrorInvalidArgument int32 = -3
