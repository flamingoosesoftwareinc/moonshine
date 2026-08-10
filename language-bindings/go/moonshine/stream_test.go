package moonshine

import (
	"testing"
	"time"

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

type fakeStreamClock struct{ now time.Time }

func (c *fakeStreamClock) Now() time.Time { return c.now }
func (c *fakeStreamClock) Advance(duration time.Duration) {
	c.now = c.now.Add(duration)
}

func newPacedStream(
	t *testing.T,
	interval time.Duration,
	cost func(pass int) time.Duration,
) (*Stream, *fakeTranscriberBindings, *fakeStreamClock) {
	t.Helper()
	clock := &fakeStreamClock{now: time.Unix(0, 0)}
	bindings := &fakeTranscriberBindings{handle: 42, streamHandle: 7}
	pass := 0
	bindings.streamTranscribeHook = func() {
		pass++
		clock.Advance(cost(pass))
	}
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, transcriber.Close()) })
	stream, err := transcriber.NewStreamWithConfig(StreamConfig{UpdateInterval: interval})
	require.NoError(t, err)
	stream.now = clock.Now
	return stream, bindings, clock
}

func feedStream(t *testing.T, stream *Stream, duration time.Duration) {
	t.Helper()
	const sampleRate = 1000
	const chunk = 100 * time.Millisecond
	samples := make([]float32, sampleRate*int(chunk)/int(time.Second))
	for elapsed := time.Duration(0); elapsed < duration; elapsed += chunk {
		require.NoError(t, stream.AddAudio(samples, sampleRate))
	}
}

func TestStreamCadenceWaitsForAudioCoveringPreviousPass(t *testing.T) {
	stream, bindings, _ := newPacedStream(t, 500*time.Millisecond, func(int) time.Duration {
		return 2 * time.Second
	})

	feedStream(t, stream, 500*time.Millisecond)
	assert.Len(t, bindings.streamTranscriptFlags, 1)
	feedStream(t, stream, 1900*time.Millisecond)
	assert.Len(t, bindings.streamTranscriptFlags, 1)
	feedStream(t, stream, 100*time.Millisecond)
	assert.Len(t, bindings.streamTranscriptFlags, 2)
}

func TestStreamCadenceKeepsFloorWhenInferenceIsFast(t *testing.T) {
	stream, bindings, _ := newPacedStream(t, 500*time.Millisecond, func(int) time.Duration {
		return 50 * time.Millisecond
	})

	feedStream(t, stream, 5*time.Second)

	assert.InDelta(t, 10, len(bindings.streamTranscriptFlags), 1)
}

func TestStreamCadenceCapsOneFreakPass(t *testing.T) {
	stream, bindings, _ := newPacedStream(t, 500*time.Millisecond, func(pass int) time.Duration {
		if pass == 1 {
			return time.Minute
		}
		return 50 * time.Millisecond
	})

	feedStream(t, stream, 500*time.Millisecond)
	assert.Len(t, bindings.streamTranscriptFlags, 1)
	feedStream(t, stream, 5100*time.Millisecond)
	assert.GreaterOrEqual(t, len(bindings.streamTranscriptFlags), 2)
	feedStream(t, stream, 2*time.Second)
	assert.GreaterOrEqual(t, len(bindings.streamTranscriptFlags), 5)
}

func TestStreamZeroCadenceUpdatesEveryAdd(t *testing.T) {
	stream, bindings, _ := newPacedStream(t, 0, func(int) time.Duration { return 0 })

	feedStream(t, stream, 500*time.Millisecond)

	assert.Len(t, bindings.streamTranscriptFlags, 5)
}

func TestStreamStopAlwaysForceFlushesWithConfiguredFlags(t *testing.T) {
	clock := &fakeStreamClock{now: time.Unix(0, 0)}
	bindings := &fakeTranscriberBindings{handle: 42, streamHandle: 7}
	bindings.streamTranscribeHook = func() { clock.Advance(5 * time.Second) }
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, transcriber.Close()) })
	stream, err := transcriber.NewStreamWithConfig(StreamConfig{
		UpdateInterval:  500 * time.Millisecond,
		TranscribeFlags: FlagSpellingMode,
	})
	require.NoError(t, err)
	stream.now = clock.Now

	feedStream(t, stream, 500*time.Millisecond)
	feedStream(t, stream, 100*time.Millisecond)
	require.NoError(t, stream.Stop())

	assert.Equal(t, []uint32{
		uint32(FlagSpellingMode),
		uint32(FlagForceUpdate | FlagSpellingMode),
	}, bindings.streamTranscriptFlags)
}

func TestStreamTranscribeFlagsCanChangeMidStream(t *testing.T) {
	stream, bindings, _ := newPacedStream(t, 0, func(int) time.Duration { return 0 })
	assert.Zero(t, stream.TranscribeFlags())
	require.NoError(t, stream.SetTranscribeFlags(FlagSpellingMode))
	assert.Equal(t, FlagSpellingMode, stream.TranscribeFlags())

	feedStream(t, stream, 100*time.Millisecond)

	assert.Equal(t, []uint32{uint32(FlagSpellingMode)}, bindings.streamTranscriptFlags)
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
