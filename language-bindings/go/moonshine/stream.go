package moonshine

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

const (
	defaultStreamUpdateInterval = 500 * time.Millisecond
	maxUpdateIntervalFactor     = 10
)

// StreamConfig controls stream creation and automatic transcript updates.
type StreamConfig struct {
	CreationFlags   TranscribeFlags
	TranscribeFlags TranscribeFlags
	// UpdateInterval is the minimum amount of audio between implicit
	// transcription passes. Zero requests a pass after every AddAudio call.
	UpdateInterval time.Duration
}

// Stream owns one native streaming transcription session. It retains its
// parent Transcriber, which must outlive the native stream. Closing the parent
// closes every remaining stream before releasing the transcriber.
type Stream struct {
	transcriber     *Transcriber
	handle          int32
	mu              sync.Mutex
	closed          bool
	active          bool
	closeOnce       sync.Once
	closeErr        error
	listenerMu      sync.Mutex
	listeners       map[uint64]func(TranscriptEvent)
	nextListener    uint64
	updateInterval  float64
	streamTime      float64
	lastUpdateTime  float64
	lastPass        float64
	transcribeFlags TranscribeFlags
	now             func() time.Time
}

// NewStream creates an independent streaming session on the transcriber.
func (t *Transcriber) NewStream(flags ...TranscribeFlags) (*Stream, error) {
	return t.newStream(StreamConfig{
		CreationFlags:  TranscribeFlags(combineTranscribeFlags(flags)),
		UpdateInterval: defaultStreamUpdateInterval,
	})
}

// NewStreamWithConfig creates a stream with explicit automatic-update
// cadence and flags. Unlike NewStream, a zero UpdateInterval means update on
// every AddAudio call.
func (t *Transcriber) NewStreamWithConfig(config StreamConfig) (*Stream, error) {
	if config.UpdateInterval < 0 {
		return nil, fmt.Errorf("negative stream update interval: %w", ErrInvalidArgument)
	}
	return t.newStream(config)
}

func (t *Transcriber) newStream(config StreamConfig) (*Stream, error) {
	if t == nil {
		return nil, ErrClosed
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, ErrClosed
	}

	handle := t.bindings.createStream(t.handle, uint32(config.CreationFlags))
	if handle < 0 {
		return nil, fmt.Errorf(
			"moonshine: create stream: %w",
			nativeError(handle, t.bindings.errorToString(handle)),
		)
	}
	stream := &Stream{
		transcriber:     t,
		handle:          handle,
		listeners:       make(map[uint64]func(TranscriptEvent)),
		updateInterval:  config.UpdateInterval.Seconds(),
		transcribeFlags: config.TranscribeFlags,
		now:             time.Now,
	}
	t.streams[handle] = struct{}{}
	runtime.SetFinalizer(stream, (*Stream).finalize)
	return stream, nil
}

// Start marks the beginning of a contiguous audio segment.
func (s *Stream) Start() error {
	return s.changeState("start", true)
}

// Stop marks the end of a contiguous audio segment.
func (s *Stream) Stop() error {
	if s == nil || s.transcriber == nil {
		return ErrClosed
	}

	t := s.transcriber
	t.mu.RLock()
	s.mu.Lock()
	if t.closed || s.closed {
		s.mu.Unlock()
		t.mu.RUnlock()
		return ErrClosed
	}
	code := t.bindings.stopStream(t.handle, s.handle)
	if code < 0 {
		s.mu.Unlock()
		t.mu.RUnlock()
		return s.nativeError("stop stream", code)
	}
	s.active = false
	transcript, code, err := s.transcriptLocked(FlagForceUpdate | s.transcribeFlags)
	s.mu.Unlock()
	t.mu.RUnlock()
	runtime.KeepAlive(s)
	if code < 0 {
		return s.nativeError("flush stream", code)
	}
	if err != nil {
		return fmt.Errorf("moonshine: copy final stream transcript: %w", err)
	}
	s.emit(transcriptEvents(transcript))
	return nil
}

func (s *Stream) changeState(operation string, active bool) error {
	if s == nil || s.transcriber == nil {
		return ErrClosed
	}

	t := s.transcriber
	t.mu.RLock()
	defer t.mu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.closed || s.closed {
		return ErrClosed
	}

	var code int32
	if active {
		code = t.bindings.startStream(t.handle, s.handle)
	} else {
		code = t.bindings.stopStream(t.handle, s.handle)
	}
	if code < 0 {
		return fmt.Errorf(
			"moonshine: %s stream: %w",
			operation,
			nativeError(code, t.bindings.errorToString(code)),
		)
	}
	s.active = active
	runtime.KeepAlive(s)
	return nil
}

// AddAudio appends mono float-PCM samples and runs an implicit transcription
// pass when the configured audio-time cadence is due.
func (s *Stream) AddAudio(audio []float32, sampleRate int, flags ...TranscribeFlags) error {
	if s == nil || s.transcriber == nil {
		return ErrClosed
	}
	if sampleRate <= 0 || uint64(sampleRate) > uint64(^uint32(0)>>1) {
		return fmt.Errorf("invalid sample rate %d: %w", sampleRate, ErrInvalidArgument)
	}

	t := s.transcriber
	t.mu.RLock()
	s.mu.Lock()
	if t.closed || s.closed {
		s.mu.Unlock()
		t.mu.RUnlock()
		return ErrClosed
	}

	code := t.bindings.addAudioToStream(
		t.handle,
		s.handle,
		audio,
		int32(sampleRate),
		combineTranscribeFlags(flags),
	)
	runtime.KeepAlive(s)
	if code < 0 {
		s.mu.Unlock()
		t.mu.RUnlock()
		return s.nativeError("add stream audio", code)
	}

	s.streamTime += float64(len(audio)) / float64(sampleRate)
	needed := min(max(s.updateInterval, s.lastPass), s.updateInterval*maxUpdateIntervalFactor)
	var transcript Transcript
	var err error
	if s.streamTime-s.lastUpdateTime >= needed {
		transcript, code, err = s.transcriptLocked(s.transcribeFlags)
		if code >= 0 && err == nil {
			s.lastUpdateTime = s.streamTime
		}
	}
	s.mu.Unlock()
	t.mu.RUnlock()
	if code < 0 {
		return s.nativeError("update stream", code)
	}
	if err != nil {
		return fmt.Errorf("moonshine: copy stream transcript: %w", err)
	}
	s.emit(transcriptEvents(transcript))
	return nil
}

// Transcript runs an incremental transcription pass and returns a Go-owned
// snapshot that remains valid after later stream calls and cleanup.
func (s *Stream) Transcript(flags ...TranscribeFlags) (Transcript, error) {
	if s == nil || s.transcriber == nil {
		return Transcript{}, ErrClosed
	}

	t := s.transcriber
	t.mu.RLock()
	s.mu.Lock()
	if t.closed || s.closed {
		s.mu.Unlock()
		t.mu.RUnlock()
		return Transcript{}, ErrClosed
	}

	transcript, code, err := s.transcriptLocked(TranscribeFlags(combineTranscribeFlags(flags)))
	runtime.KeepAlive(s)
	s.mu.Unlock()
	t.mu.RUnlock()
	if code < 0 {
		return Transcript{}, fmt.Errorf(
			"moonshine: transcribe stream: %w",
			nativeError(code, t.bindings.errorToString(code)),
		)
	}
	if err != nil {
		return Transcript{}, fmt.Errorf("moonshine: copy stream transcript: %w", err)
	}
	s.emit(transcriptEvents(transcript))
	return transcript, nil
}

func (s *Stream) transcriptLocked(flags TranscribeFlags) (Transcript, int32, error) {
	started := s.now()
	transcript, code, err := s.transcriber.bindings.transcribeStream(
		s.transcriber.handle,
		s.handle,
		uint32(flags),
	)
	s.lastPass = s.now().Sub(started).Seconds()
	return transcript, code, err
}

func (s *Stream) nativeError(operation string, code int32) error {
	return fmt.Errorf(
		"moonshine: %s: %w",
		operation,
		nativeError(code, s.transcriber.bindings.errorToString(code)),
	)
}

// TranscribeFlags returns the flags used for implicit updates.
func (s *Stream) TranscribeFlags() TranscribeFlags {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transcribeFlags
}

// SetTranscribeFlags changes the flags used by subsequent implicit updates.
func (s *Stream) SetTranscribeFlags(flags TranscribeFlags) error {
	if s == nil || s.transcriber == nil {
		return ErrClosed
	}
	t := s.transcriber
	t.mu.RLock()
	defer t.mu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.closed || s.closed {
		return ErrClosed
	}
	s.transcribeFlags = flags
	return nil
}

// AddListener subscribes to events derived from successful Transcript calls.
// Delivery is synchronous and follows transcript line order. The returned
// function removes only this listener and is safe to call repeatedly.
func (s *Stream) AddListener(listener func(TranscriptEvent)) (remove func()) {
	if s == nil || listener == nil {
		return func() {}
	}
	s.listenerMu.Lock()
	id := s.nextListener
	s.nextListener++
	s.listeners[id] = listener
	s.listenerMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			s.listenerMu.Lock()
			delete(s.listeners, id)
			s.listenerMu.Unlock()
		})
	}
}

// RemoveAllListeners removes every event listener from the stream.
func (s *Stream) RemoveAllListeners() {
	if s == nil {
		return
	}
	s.listenerMu.Lock()
	clear(s.listeners)
	s.listenerMu.Unlock()
}

func (s *Stream) emit(events []TranscriptEvent) {
	s.listenerMu.Lock()
	listeners := make([]func(TranscriptEvent), 0, len(s.listeners))
	for _, listener := range s.listeners {
		listeners = append(listeners, listener)
	}
	s.listenerMu.Unlock()

	for _, event := range events {
		for _, listener := range listeners {
			listener(event)
		}
	}
}

// Close releases the native stream. It is safe to call Close more than once.
func (s *Stream) Close() error {
	if s == nil || s.transcriber == nil {
		return nil
	}

	s.closeOnce.Do(func() {
		t := s.transcriber
		t.mu.Lock()
		defer t.mu.Unlock()
		s.mu.Lock()
		defer s.mu.Unlock()

		runtime.SetFinalizer(s, nil)
		s.closed = true
		s.active = false
		s.RemoveAllListeners()
		if t.closed {
			return
		}
		code := t.bindings.freeStream(t.handle, s.handle)
		delete(t.streams, s.handle)
		if code < 0 {
			s.closeErr = fmt.Errorf(
				"moonshine: free stream: %w",
				nativeError(code, t.bindings.errorToString(code)),
			)
		}
	})
	return s.closeErr
}

func (s *Stream) finalize() {
	_ = s.Close()
}
