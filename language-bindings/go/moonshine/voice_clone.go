package moonshine

import (
	"sync"
)

const (
	// VoiceCloneSampleRate is the sample rate of completed reference clips.
	VoiceCloneSampleRate            = 16000
	voiceCloneSearchIntervalSeconds = 0.25
)

// VoiceClone incrementally finds a short, speech-heavy reference recording.
// It borrows and retains its TextToSpeech owner; closing that owner makes
// future searches return ErrClosed.
type VoiceClone struct {
	tts     *TextToSpeech
	options []Option

	mu                  sync.RWMutex
	searchMu            sync.Mutex
	recording           []float32
	recordingSampleRate int
	samplesSinceSearch  int
	clip                []float32
	transcript          string
	speechSeconds       float32
	readyHandlers       []func()
	progressHandlers    []func(recordedSeconds, speechSeconds float64)
}

// StartVoiceClone creates an incremental reference-clip capture. Options are
// forwarded to ExtractSpeechClip, including clip_duration_seconds,
// minimum_speech_seconds, vad_threshold, and tail_pad_seconds.
func (t *TextToSpeech) StartVoiceClone(options ...Option) (*VoiceClone, error) {
	if t == nil {
		return nil, ErrClosed
	}
	if err := validateOptions(options); err != nil {
		return nil, err
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.closed {
		return nil, ErrClosed
	}
	return &VoiceClone{
		tts: t, options: append([]Option(nil), options...),
		recordingSampleRate: VoiceCloneSampleRate,
	}, nil
}

// OnReady registers a handler that fires once when a clip is ready. A handler
// registered after readiness is invoked immediately.
func (v *VoiceClone) OnReady(handler func()) *VoiceClone {
	if v == nil || handler == nil {
		return v
	}
	v.mu.Lock()
	ready := v.clip != nil
	if !ready {
		v.readyHandlers = append(v.readyHandlers, handler)
	}
	v.mu.Unlock()
	if ready {
		handler()
	}
	return v
}

// OnProgress registers a handler called after each clip search.
func (v *VoiceClone) OnProgress(
	handler func(recordedSeconds, speechSeconds float64),
) *VoiceClone {
	if v == nil || handler == nil {
		return v
	}
	v.mu.Lock()
	v.progressHandlers = append(v.progressHandlers, handler)
	v.mu.Unlock()
	return v
}

// AddAudio appends captured mono PCM. Searches run at most four times per
// second; changing sample rate discards the prior recording.
func (v *VoiceClone) AddAudio(pcm []float32, sampleRate int) error {
	if v == nil {
		return ErrClosed
	}
	v.mu.Lock()
	if v.clip != nil || len(pcm) == 0 || sampleRate <= 0 {
		v.mu.Unlock()
		return nil
	}
	if sampleRate != v.recordingSampleRate {
		v.recording = v.recording[:0]
		v.recordingSampleRate = sampleRate
		v.samplesSinceSearch = 0
	}
	v.recording = append(v.recording, pcm...)
	v.samplesSinceSearch += len(pcm)
	due := float64(v.samplesSinceSearch) >= voiceCloneSearchIntervalSeconds*float64(sampleRate)
	if due {
		v.samplesSinceSearch = 0
	}
	v.mu.Unlock()
	if !due {
		return nil
	}
	return v.search()
}

func (v *VoiceClone) search() error {
	v.searchMu.Lock()
	defer v.searchMu.Unlock()
	v.mu.RLock()
	if v.clip != nil || len(v.recording) == 0 {
		v.mu.RUnlock()
		return nil
	}
	samples := append([]float32(nil), v.recording...)
	rate := v.recordingSampleRate
	v.mu.RUnlock()

	result, err := v.tts.ExtractSpeechClip(samples, rate, v.options...)
	if err != nil {
		return err
	}
	v.mu.Lock()
	v.speechSeconds = result.Duration
	recorded := float64(len(samples)) / float64(rate)
	progressHandlers := append(
		[]func(float64, float64){}, v.progressHandlers...,
	)
	var readyHandlers []func()
	if result.Complete && len(result.Audio.Samples) > 0 && v.clip == nil {
		v.clip = append([]float32(nil), result.Audio.Samples...)
		v.transcript = result.Transcript
		readyHandlers = append([]func(){}, v.readyHandlers...)
		v.readyHandlers = nil
	}
	speech := float64(v.speechSeconds)
	v.mu.Unlock()

	for _, handler := range progressHandlers {
		handler(recorded, speech)
	}
	for _, handler := range readyHandlers {
		handler()
	}
	return nil
}

// Reset discards all recorded audio and any completed clip.
func (v *VoiceClone) Reset() {
	if v == nil {
		return
	}
	v.searchMu.Lock()
	defer v.searchMu.Unlock()
	v.mu.Lock()
	v.recording = nil
	v.samplesSinceSearch = 0
	v.clip = nil
	v.transcript = ""
	v.speechSeconds = 0
	v.mu.Unlock()
}

func (v *VoiceClone) IsReady() bool {
	if v == nil {
		return false
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.clip != nil
}

func (v *VoiceClone) Audio() Audio {
	if v == nil {
		return Audio{}
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.clip == nil {
		return Audio{}
	}
	return Audio{Samples: append([]float32(nil), v.clip...), SampleRate: VoiceCloneSampleRate}
}

func (v *VoiceClone) Transcript() string {
	if v == nil {
		return ""
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.transcript
}

func (v *VoiceClone) SpeechSeconds() float64 {
	if v == nil {
		return 0
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	return float64(v.speechSeconds)
}

func (v *VoiceClone) RecordedSeconds() float64 {
	if v == nil {
		return 0
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.recordingSampleRate <= 0 {
		return 0
	}
	return float64(len(v.recording)) / float64(v.recordingSampleRate)
}
