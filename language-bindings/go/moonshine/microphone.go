package moonshine

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// AudioCapture is a platform microphone adapter. Start must return after
// capture begins and invoke callback for each mono float-PCM block.
type AudioCapture interface {
	Start(callback func(Audio)) error
	Stop() error
	Close() error
}

// MicTranscriberConfig controls ownership of injected resources.
type MicTranscriberConfig struct {
	OwnStream  bool
	OwnCapture bool
}

type microphoneStream interface {
	Start() error
	Stop() error
	AddAudio(audio []float32, sampleRate int, flags ...TranscribeFlags) error
	AddListener(listener func(TranscriptEvent)) func()
	RemoveAllListeners()
	Close() error
}

// MicTranscriber moves microphone blocks onto a worker before feeding a
// transcription stream, keeping inference off the capture callback thread.
type MicTranscriber struct {
	stream  microphoneStream
	capture AudioCapture
	config  MicTranscriberConfig

	mu                sync.Mutex
	run               *micRun
	closed            bool
	errorMu           sync.Mutex
	errorListeners    map[uint64]func(error)
	nextErrorListener uint64
}

type micRun struct {
	owner *MicTranscriber

	mu        sync.Mutex
	pending   []Audio
	accepting bool
	stopping  bool
	wake      chan struct{}
	done      chan struct{}
	err       error
	stopOnce  sync.Once
}

// NewMicTranscriber composes an existing stream with a platform capture
// backend. Construction has no side effects; Start opens capture explicitly.
func NewMicTranscriber(
	stream *Stream,
	capture AudioCapture,
	config MicTranscriberConfig,
) (*MicTranscriber, error) {
	return newMicTranscriber(stream, capture, config)
}

func newMicTranscriber(
	stream microphoneStream,
	capture AudioCapture,
	config MicTranscriberConfig,
) (*MicTranscriber, error) {
	if stream == nil || capture == nil {
		return nil, fmt.Errorf("microphone stream and capture are required: %w", ErrInvalidArgument)
	}
	return &MicTranscriber{
		stream: stream, capture: capture, config: config,
		errorListeners: make(map[uint64]func(error)),
	}, nil
}

// Start begins stream and microphone capture. Repeated calls while running are
// idempotent. Cancelling ctx asynchronously stops and drains the current run.
func (m *MicTranscriber) Start(ctx context.Context) error {
	if m == nil {
		return ErrClosed
	}
	if ctx == nil {
		return fmt.Errorf("nil microphone context: %w", ErrInvalidArgument)
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	if m.run != nil {
		m.mu.Unlock()
		return nil
	}
	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		return err
	}
	if err := m.stream.Start(); err != nil {
		m.mu.Unlock()
		return err
	}
	run := &micRun{
		owner: m, accepting: true, wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	m.run = run
	m.mu.Unlock()
	go run.work()
	if err := m.capture.Start(run.capture); err != nil {
		_ = m.stopRun(run)
		return fmt.Errorf("start microphone capture: %w", err)
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = m.stopRun(run)
		case <-run.done:
		}
	}()
	return nil
}

func (r *micRun) capture(audio Audio) {
	if len(audio.Samples) == 0 || audio.SampleRate <= 0 {
		return
	}
	samples := append([]float32(nil), audio.Samples...)
	r.mu.Lock()
	if !r.accepting {
		r.mu.Unlock()
		return
	}
	if count := len(r.pending); count > 0 && r.pending[count-1].SampleRate == audio.SampleRate {
		r.pending[count-1].Samples = append(r.pending[count-1].Samples, samples...)
	} else {
		r.pending = append(r.pending, Audio{Samples: samples, SampleRate: audio.SampleRate})
	}
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *micRun) work() {
	defer close(r.done)
	for {
		<-r.wake
		for {
			r.mu.Lock()
			pending := r.pending
			r.pending = nil
			stopping := r.stopping
			r.mu.Unlock()
			for _, audio := range pending {
				if err := r.owner.stream.AddAudio(audio.Samples, audio.SampleRate); err != nil {
					r.mu.Lock()
					r.err = errors.Join(r.err, err)
					r.accepting = false
					r.stopping = true
					r.mu.Unlock()
					return
				}
			}
			r.mu.Lock()
			empty := len(r.pending) == 0
			stopping = stopping || r.stopping
			r.mu.Unlock()
			if stopping && empty {
				return
			}
			if empty {
				break
			}
		}
	}
}

// Stop stops capture, drains every accepted block, and flushes the stream.
func (m *MicTranscriber) Stop() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	run := m.run
	m.mu.Unlock()
	if run == nil {
		return nil
	}
	return m.stopRun(run)
}

func (m *MicTranscriber) stopRun(run *micRun) error {
	run.stopOnce.Do(func() {
		run.mu.Lock()
		run.accepting = false
		run.mu.Unlock()
		captureErr := m.capture.Stop()
		run.mu.Lock()
		run.stopping = true
		run.mu.Unlock()
		select {
		case run.wake <- struct{}{}:
		default:
		}
		<-run.done
		run.mu.Lock()
		workerErr := run.err
		run.mu.Unlock()
		streamErr := m.stream.Stop()
		run.mu.Lock()
		run.err = errors.Join(captureErr, workerErr, streamErr)
		runErr := run.err
		run.mu.Unlock()
		m.mu.Lock()
		if m.run == run {
			m.run = nil
		}
		m.mu.Unlock()
		m.emitError(runErr)
	})
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.err
}

// AddErrorListener subscribes to asynchronous capture, worker, and stop
// failures. The returned function removes only this listener.
func (m *MicTranscriber) AddErrorListener(listener func(error)) func() {
	if m == nil || listener == nil {
		return func() {}
	}
	m.errorMu.Lock()
	id := m.nextErrorListener
	m.nextErrorListener++
	m.errorListeners[id] = listener
	m.errorMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			m.errorMu.Lock()
			delete(m.errorListeners, id)
			m.errorMu.Unlock()
		})
	}
}

func (m *MicTranscriber) emitError(err error) {
	if err == nil {
		return
	}
	m.errorMu.Lock()
	listeners := make([]func(error), 0, len(m.errorListeners))
	for _, listener := range m.errorListeners {
		listeners = append(listeners, listener)
	}
	m.errorMu.Unlock()
	for _, listener := range listeners {
		listener(err)
	}
}

// AddListener subscribes to the underlying transcript event stream.
func (m *MicTranscriber) AddListener(listener func(TranscriptEvent)) func() {
	if m == nil || listener == nil {
		return func() {}
	}
	return m.stream.AddListener(listener)
}

// Close stops capture and releases resources configured as owned.
func (m *MicTranscriber) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()
	err := m.Stop()
	m.stream.RemoveAllListeners()
	m.errorMu.Lock()
	clear(m.errorListeners)
	m.errorMu.Unlock()
	if m.config.OwnCapture {
		err = errors.Join(err, m.capture.Close())
	}
	if m.config.OwnStream {
		err = errors.Join(err, m.stream.Close())
	}
	return err
}
