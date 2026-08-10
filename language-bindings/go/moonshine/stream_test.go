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

func TestStreamAddAudioForwardsSamplesRateAndFlags(t *testing.T) {
	bindings := &fakeTranscriberBindings{handle: 42, streamHandle: 7}
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, transcriber.Close()) })
	stream, err := transcriber.NewStream()
	require.NoError(t, err)

	err = stream.AddAudio([]float32{0.25, -0.5}, 16000, FlagSpellingMode)

	require.NoError(t, err)
	assert.Equal(t, [][]float32{{0.25, -0.5}}, bindings.streamAudio)
	assert.Equal(t, []int32{16000}, bindings.streamRates)
	assert.Equal(t, []uint32{uint32(FlagSpellingMode)}, bindings.streamAudioFlags)
}

func TestStreamTranscriptReturnsSnapshotAndForwardsFlags(t *testing.T) {
	want := Transcript{Lines: []TranscriptLine{{Text: "hello"}}}
	bindings := &fakeTranscriberBindings{handle: 42, streamHandle: 7, streamTranscript: want}
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, transcriber.Close()) })
	stream, err := transcriber.NewStream()
	require.NoError(t, err)

	got, err := stream.Transcript(FlagForceUpdate, FlagSpellingMode)

	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, []uint32{uint32(FlagForceUpdate | FlagSpellingMode)}, bindings.streamTranscriptFlags)
}

func TestStreamAudioAndTranscriptMapNativeErrors(t *testing.T) {
	bindings := &fakeTranscriberBindings{handle: 42, streamHandle: 7}
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)
	stream, err := transcriber.NewStream()
	require.NoError(t, err)
	bindings.streamCode = rawErrorInvalidHandle

	require.ErrorIs(t, stream.AddAudio(nil, 16000), ErrInvalidHandle)
	_, err = stream.Transcript()
	require.ErrorIs(t, err, ErrInvalidHandle)

	bindings.streamCode = 0
	require.NoError(t, stream.Close())
	require.NoError(t, transcriber.Close())
}

func TestStreamAddAudioRejectsInvalidSampleRate(t *testing.T) {
	bindings := &fakeTranscriberBindings{handle: 42, streamHandle: 7}
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, transcriber.Close()) })
	stream, err := transcriber.NewStream()
	require.NoError(t, err)

	for _, sampleRate := range []int{0, -1} {
		err = stream.AddAudio(nil, sampleRate)
		require.ErrorIs(t, err, ErrInvalidArgument)
	}
	assert.Empty(t, bindings.streamAudio)
}

func TestClosedStreamRejectsAudioAndTranscript(t *testing.T) {
	bindings := &fakeTranscriberBindings{handle: 42, streamHandle: 7}
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, transcriber.Close()) })
	stream, err := transcriber.NewStream()
	require.NoError(t, err)
	require.NoError(t, stream.Close())

	require.ErrorIs(t, stream.AddAudio(nil, 16000), ErrClosed)
	_, err = stream.Transcript()
	require.ErrorIs(t, err, ErrClosed)
}

func TestStreamTranscriptEmitsEventsAndSupportsRemoval(t *testing.T) {
	line := TranscriptLine{Text: "hello", IsNew: true, IsUpdated: true, IsComplete: true}
	bindings := &fakeTranscriberBindings{
		handle:           42,
		streamHandle:     7,
		streamTranscript: Transcript{Lines: []TranscriptLine{line}},
	}
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, transcriber.Close()) })
	stream, err := transcriber.NewStream()
	require.NoError(t, err)

	var got []TranscriptEvent
	remove := stream.AddListener(func(event TranscriptEvent) { got = append(got, event) })
	_, err = stream.Transcript()
	require.NoError(t, err)
	remove()
	remove()
	_, err = stream.Transcript()
	require.NoError(t, err)

	assert.Equal(t, []TranscriptEvent{
		LineStarted{Line: line},
		LineCompleted{Line: line},
	}, got)
}

func TestStreamListenerCanCallBackIntoStream(t *testing.T) {
	line := TranscriptLine{Text: "hello", IsNew: true}
	bindings := &fakeTranscriberBindings{
		handle:           42,
		streamHandle:     7,
		streamTranscript: Transcript{Lines: []TranscriptLine{line}},
	}
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, transcriber.Close()) })
	stream, err := transcriber.NewStream()
	require.NoError(t, err)

	called := false
	stream.AddListener(func(TranscriptEvent) {
		called = true
		stream.RemoveAllListeners()
	})
	_, err = stream.Transcript()

	require.NoError(t, err)
	assert.True(t, called)
}
