package moonshine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAudioCapture struct {
	mu         sync.Mutex
	callback   func(Audio)
	startCalls int
	stopCalls  int
	closeCalls int
	startErr   error
	stopErr    error
	closeErr   error
}

func (f *fakeAudioCapture) Start(callback func(Audio)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls++
	f.callback = callback
	return f.startErr
}
func (f *fakeAudioCapture) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls++
	return f.stopErr
}
func (f *fakeAudioCapture) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCalls++
	return f.closeErr
}
func (f *fakeAudioCapture) emit(audio Audio) {
	f.mu.Lock()
	callback := f.callback
	f.mu.Unlock()
	if callback != nil {
		callback(audio)
	}
}

type fakeMicrophoneStream struct {
	mu          sync.Mutex
	startCalls  int
	stopCalls   int
	closeCalls  int
	removeCalls int
	addCalls    int
	total       int
	rates       []int
	startErr    error
	stopErr     error
	addErr      error
	closeErr    error
	block       <-chan struct{}
	entered     chan struct{}
	listener    func(TranscriptEvent)
}

func (f *fakeMicrophoneStream) Start() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls++
	return f.startErr
}
func (f *fakeMicrophoneStream) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls++
	return f.stopErr
}
func (f *fakeMicrophoneStream) AddAudio(audio []float32, sampleRate int, _ ...TranscribeFlags) error {
	f.mu.Lock()
	f.addCalls++
	f.total += len(audio)
	f.rates = append(f.rates, sampleRate)
	block := f.block
	entered := f.entered
	err := f.addErr
	f.mu.Unlock()
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if block != nil {
		<-block
	}
	return err
}
func (f *fakeMicrophoneStream) AddListener(listener func(TranscriptEvent)) func() {
	f.mu.Lock()
	f.listener = listener
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.listener = nil
		f.mu.Unlock()
	}
}
func (f *fakeMicrophoneStream) RemoveAllListeners() {
	f.mu.Lock()
	f.removeCalls++
	f.listener = nil
	f.mu.Unlock()
}
func (f *fakeMicrophoneStream) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCalls++
	return f.closeErr
}

func TestNewMicTranscriberHasNoSideEffects(t *testing.T) {
	stream := &fakeMicrophoneStream{}
	capture := &fakeAudioCapture{}
	mic, err := newMicTranscriber(stream, capture, MicTranscriberConfig{})
	require.NoError(t, err)
	assert.NotNil(t, mic)
	assert.Zero(t, stream.startCalls)
	assert.Zero(t, capture.startCalls)

	_, err = newMicTranscriber(nil, capture, MicTranscriberConfig{})
	require.ErrorIs(t, err, ErrInvalidArgument)
	_, err = newMicTranscriber(stream, nil, MicTranscriberConfig{})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestMicTranscriberCaptureCallbackDoesNotBlockAndDropsNoAudio(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	stream := &fakeMicrophoneStream{block: release, entered: entered}
	capture := &fakeAudioCapture{}
	mic, err := newMicTranscriber(stream, capture, MicTranscriberConfig{})
	require.NoError(t, err)
	require.NoError(t, mic.Start(context.Background()))

	capture.emit(Audio{Samples: make([]float32, 100), SampleRate: 16000})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not enter AddAudio")
	}
	started := time.Now()
	for range 9 {
		capture.emit(Audio{Samples: make([]float32, 100), SampleRate: 16000})
	}
	assert.Less(t, time.Since(started), 50*time.Millisecond)
	close(release)
	require.NoError(t, mic.Stop())

	stream.mu.Lock()
	defer stream.mu.Unlock()
	assert.Equal(t, 1000, stream.total)
	assert.Less(t, stream.addCalls, 10)
	assert.Equal(t, 1, stream.startCalls)
	assert.Equal(t, 1, stream.stopCalls)
}

func TestMicTranscriberPreservesSampleRateBoundaries(t *testing.T) {
	stream := &fakeMicrophoneStream{}
	capture := &fakeAudioCapture{}
	mic, err := newMicTranscriber(stream, capture, MicTranscriberConfig{})
	require.NoError(t, err)
	require.NoError(t, mic.Start(context.Background()))
	capture.emit(Audio{Samples: []float32{1}, SampleRate: 16000})
	capture.emit(Audio{Samples: []float32{2}, SampleRate: 8000})
	capture.emit(Audio{Samples: []float32{3}, SampleRate: 8000})
	require.NoError(t, mic.Stop())
	stream.mu.Lock()
	defer stream.mu.Unlock()
	require.NotEmpty(t, stream.rates)
	assert.Equal(t, 16000, stream.rates[0])
	for _, rate := range stream.rates[1:] {
		assert.Equal(t, 8000, rate)
	}
	assert.Equal(t, 3, stream.total)
}

func TestMicTranscriberContextCancellationStopsCurrentRun(t *testing.T) {
	stream := &fakeMicrophoneStream{}
	capture := &fakeAudioCapture{}
	mic, err := newMicTranscriber(stream, capture, MicTranscriberConfig{})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, mic.Start(ctx))
	cancel()
	require.Eventually(t, func() bool {
		mic.mu.Lock()
		defer mic.mu.Unlock()
		return mic.run == nil
	}, time.Second, time.Millisecond)
	assert.Equal(t, 1, capture.stopCalls)
	assert.Equal(t, 1, stream.stopCalls)
}

func TestMicTranscriberCloseHonorsOwnershipAndIsIdempotent(t *testing.T) {
	stream := &fakeMicrophoneStream{}
	capture := &fakeAudioCapture{}
	mic, err := newMicTranscriber(stream, capture, MicTranscriberConfig{
		OwnStream: true, OwnCapture: true,
	})
	require.NoError(t, err)
	require.NoError(t, mic.Start(context.Background()))
	require.NoError(t, mic.Close())
	require.NoError(t, mic.Close())
	assert.Equal(t, 1, stream.closeCalls)
	assert.Equal(t, 1, capture.closeCalls)
	assert.Equal(t, 1, stream.removeCalls)
	require.ErrorIs(t, mic.Start(context.Background()), ErrClosed)

	borrowedStream := &fakeMicrophoneStream{}
	borrowedCapture := &fakeAudioCapture{}
	borrowed, err := newMicTranscriber(
		borrowedStream, borrowedCapture, MicTranscriberConfig{},
	)
	require.NoError(t, err)
	require.NoError(t, borrowed.Close())
	assert.Zero(t, borrowedStream.closeCalls)
	assert.Zero(t, borrowedCapture.closeCalls)
}

func TestMicTranscriberJoinsLifecycleErrors(t *testing.T) {
	startErr := errors.New("capture start")
	stream := &fakeMicrophoneStream{}
	capture := &fakeAudioCapture{startErr: startErr}
	mic, err := newMicTranscriber(stream, capture, MicTranscriberConfig{})
	require.NoError(t, err)
	require.ErrorIs(t, mic.Start(context.Background()), startErr)
	assert.Equal(t, 1, stream.stopCalls)

	addErr := errors.New("add audio")
	stopErr := errors.New("capture stop")
	stream = &fakeMicrophoneStream{addErr: addErr}
	capture = &fakeAudioCapture{stopErr: stopErr}
	mic, err = newMicTranscriber(stream, capture, MicTranscriberConfig{})
	require.NoError(t, err)
	var seen error
	remove := mic.AddErrorListener(func(err error) { seen = err })
	require.NoError(t, mic.Start(context.Background()))
	capture.emit(Audio{Samples: []float32{1}, SampleRate: 16000})
	err = mic.Stop()
	require.ErrorIs(t, err, addErr)
	require.ErrorIs(t, err, stopErr)
	require.ErrorIs(t, seen, addErr)
	require.ErrorIs(t, seen, stopErr)
	remove()
}

func TestMicTranscriberListenerDelegatesToStream(t *testing.T) {
	stream := &fakeMicrophoneStream{}
	mic, err := newMicTranscriber(stream, &fakeAudioCapture{}, MicTranscriberConfig{})
	require.NoError(t, err)
	seen := 0
	remove := mic.AddListener(func(TranscriptEvent) { seen++ })
	stream.mu.Lock()
	listener := stream.listener
	stream.mu.Unlock()
	listener(LineStarted{})
	assert.Equal(t, 1, seen)
	remove()
	stream.mu.Lock()
	defer stream.mu.Unlock()
	assert.Nil(t, stream.listener)
}
