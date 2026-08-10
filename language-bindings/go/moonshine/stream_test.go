package moonshine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranscriberNewStreamLifecycle(t *testing.T) {
	bindings := &fakeTranscriberBindings{handle: 42, streamHandle: 7}
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)
	stream, err := transcriber.NewStream(FlagSpellingMode)
	require.NoError(t, err)

	require.NoError(t, stream.Start())
	require.NoError(t, stream.Stop())
	require.NoError(t, stream.Close())
	require.NoError(t, stream.Close())
	require.NoError(t, transcriber.Close())

	assert.Equal(t, []string{"create", "start", "stop", "free"}, bindings.streamCalls)
	assert.Equal(t, []uint32{uint32(FlagSpellingMode)}, bindings.streamFlags)
	assert.Equal(t, [][2]int32{{42, 7}}, bindings.freedStreams)
	assert.Equal(t, []int32{42}, bindings.freed)
}

func TestTranscriberCloseFreesStreamsBeforeParent(t *testing.T) {
	bindings := &fakeTranscriberBindings{handle: 42, streamHandle: 7}
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)
	stream, err := transcriber.NewStream()
	require.NoError(t, err)

	require.NoError(t, transcriber.Close())

	assert.Equal(t, []string{"create", "free"}, bindings.streamCalls)
	assert.Equal(t, [][2]int32{{42, 7}}, bindings.freedStreams)
	require.ErrorIs(t, stream.Start(), ErrClosed)
	require.ErrorIs(t, stream.Stop(), ErrClosed)
	require.NoError(t, stream.Close())
}

func TestTranscriberNewStreamMapsNativeError(t *testing.T) {
	bindings := &fakeTranscriberBindings{
		handle:       42,
		streamHandle: rawErrorInvalidHandle,
		errorMessage: "Invalid handle",
	}
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, transcriber.Close()) })

	stream, err := transcriber.NewStream()

	require.ErrorIs(t, err, ErrInvalidHandle)
	assert.Nil(t, stream)
}

func TestStreamOperationsMapNativeErrors(t *testing.T) {
	bindings := &fakeTranscriberBindings{handle: 42, streamHandle: 7}
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)
	stream, err := transcriber.NewStream()
	require.NoError(t, err)

	bindings.streamCode = rawErrorInvalidHandle
	require.ErrorIs(t, stream.Start(), ErrInvalidHandle)
	require.ErrorIs(t, stream.Stop(), ErrInvalidHandle)
	require.ErrorIs(t, stream.Close(), ErrInvalidHandle)
	require.NoError(t, transcriber.Close())
}

func TestStreamOperationsAfterClose(t *testing.T) {
	bindings := &fakeTranscriberBindings{handle: 42, streamHandle: 7}
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, transcriber.Close()) })
	stream, err := transcriber.NewStream()
	require.NoError(t, err)
	require.NoError(t, stream.Close())

	require.ErrorIs(t, stream.Start(), ErrClosed)
	require.ErrorIs(t, stream.Stop(), ErrClosed)
}

func TestNilStreamLifecycle(t *testing.T) {
	var stream *Stream
	require.ErrorIs(t, stream.Start(), ErrClosed)
	require.ErrorIs(t, stream.Stop(), ErrClosed)
	require.NoError(t, stream.Close())
}
