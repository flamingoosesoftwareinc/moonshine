package moonshine

import (
	"fmt"
	"runtime"
	"sync"
)

// Stream owns one native streaming transcription session. It retains its
// parent Transcriber, which must outlive the native stream. Closing the parent
// closes every remaining stream before releasing the transcriber.
type Stream struct {
	transcriber *Transcriber
	handle      int32
	mu          sync.Mutex
	closed      bool
	active      bool
	closeOnce   sync.Once
	closeErr    error
}

// NewStream creates an independent streaming session on the transcriber.
func (t *Transcriber) NewStream(flags ...TranscribeFlags) (*Stream, error) {
	if t == nil {
		return nil, ErrClosed
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, ErrClosed
	}

	handle := t.bindings.createStream(t.handle, combineTranscribeFlags(flags))
	if handle < 0 {
		return nil, fmt.Errorf(
			"moonshine: create stream: %w",
			nativeError(handle, t.bindings.errorToString(handle)),
		)
	}
	stream := &Stream{transcriber: t, handle: handle}
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
	return s.changeState("stop", false)
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

// AddAudio appends mono float-PCM samples to the stream without running
// inference.
func (s *Stream) AddAudio(audio []float32, sampleRate int, flags ...TranscribeFlags) error {
	if s == nil || s.transcriber == nil {
		return ErrClosed
	}
	if sampleRate <= 0 || uint64(sampleRate) > uint64(^uint32(0)>>1) {
		return fmt.Errorf("invalid sample rate %d: %w", sampleRate, ErrInvalidArgument)
	}

	t := s.transcriber
	t.mu.RLock()
	defer t.mu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.closed || s.closed {
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
		return fmt.Errorf(
			"moonshine: add stream audio: %w",
			nativeError(code, t.bindings.errorToString(code)),
		)
	}
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
	defer t.mu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.closed || s.closed {
		return Transcript{}, ErrClosed
	}

	transcript, code, err := t.bindings.transcribeStream(
		t.handle,
		s.handle,
		combineTranscribeFlags(flags),
	)
	runtime.KeepAlive(s)
	if code < 0 {
		return Transcript{}, fmt.Errorf(
			"moonshine: transcribe stream: %w",
			nativeError(code, t.bindings.errorToString(code)),
		)
	}
	if err != nil {
		return Transcript{}, fmt.Errorf("moonshine: copy stream transcript: %w", err)
	}
	return transcript, nil
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
