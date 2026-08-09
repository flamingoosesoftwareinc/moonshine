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
	freed        []int32
}

func (f *fakeTranscriberBindings) loadTranscriberFromFiles(path string, modelArch uint32) int32 {
	f.paths = append(f.paths, path)
	f.arches = append(f.arches, modelArch)
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

	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)
	assert.Equal(t, []string{"/models/tiny-en"}, bindings.paths)
	assert.Equal(t, []uint32{uint32(ModelArchTiny)}, bindings.arches)

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
	require.EqualError(t, err, `moonshine: load transcriber from "/missing": moonshine error -3: Invalid argument`)
	assert.Nil(t, transcriber)
	assert.Empty(t, bindings.freed)

	var nativeErr *Error
	require.ErrorAs(t, err, &nativeErr)
	assert.Equal(t, ErrorInvalidArgument, nativeErr.Code)
}

func TestNilTranscriberClose(t *testing.T) {
	var transcriber *Transcriber
	transcriber.Close()
}

const rawErrorInvalidArgument int32 = -3
