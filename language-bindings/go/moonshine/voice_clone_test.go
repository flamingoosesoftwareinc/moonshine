package moonshine

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVoiceCloneBecomesReadyAndFiresHandlersOutsideLock(t *testing.T) {
	readyClip := SpeechClip{
		Audio:    Audio{Samples: []float32{0.1, 0.2}, SampleRate: VoiceCloneSampleRate},
		Duration: 1.5, Complete: true, Transcript: "hello",
	}
	bindings := &fakeTextToSpeechBindings{
		handle: 42,
		clips:  []SpeechClip{{Duration: 0.5}, readyClip},
	}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, synthesizer.Close()) })
	clone, err := synthesizer.StartVoiceClone(
		Option{Name: "clip_duration_seconds", Value: "2"},
	)
	require.NoError(t, err)
	var ready int
	var progress []float64
	clone.OnReady(func() {
		ready++
		_ = clone.IsReady()
	}).OnProgress(func(recorded, _ float64) {
		progress = append(progress, recorded)
		_ = clone.RecordedSeconds()
	})

	require.NoError(t, clone.AddAudio(make([]float32, 4000), 16000))
	assert.False(t, clone.IsReady())
	require.NoError(t, clone.AddAudio(make([]float32, 4000), 16000))

	assert.True(t, clone.IsReady())
	assert.Equal(t, Audio{Samples: []float32{0.1, 0.2}, SampleRate: 16000}, clone.Audio())
	assert.Equal(t, "hello", clone.Transcript())
	assert.Equal(t, 1.5, clone.SpeechSeconds())
	assert.Equal(t, 1, ready)
	assert.Equal(t, []float64{0.25, 0.5}, progress)
	assert.Equal(t, [][]Option{
		{{Name: "clip_duration_seconds", Value: "2"}},
		{{Name: "clip_duration_seconds", Value: "2"}},
	}, bindings.clipOptions)

	late := 0
	clone.OnReady(func() { late++ })
	assert.Equal(t, 1, late)
	require.NoError(t, clone.AddAudio(make([]float32, 16000), 16000))
	assert.Len(t, bindings.clipAudio, 2)
	assert.Equal(t, 1, ready)
}

func TestVoiceCloneSearchCadenceAndSampleRateReset(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{handle: 42}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, synthesizer.Close()) })
	clone, err := synthesizer.StartVoiceClone()
	require.NoError(t, err)

	for range 100 {
		require.NoError(t, clone.AddAudio(make([]float32, 160), 16000))
	}
	assert.Len(t, bindings.clipAudio, 4)
	assert.Equal(t, 1.0, clone.RecordedSeconds())
	require.NoError(t, clone.AddAudio(make([]float32, 8000), 8000))
	assert.Equal(t, 1.0, clone.RecordedSeconds())
}

func TestVoiceCloneResetAndAudioReturnsCopies(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{
		handle: 42,
		clip: SpeechClip{
			Audio: Audio{Samples: []float32{0.1}, SampleRate: 16000}, Complete: true,
		},
	}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, synthesizer.Close()) })
	clone, err := synthesizer.StartVoiceClone()
	require.NoError(t, err)
	require.NoError(t, clone.AddAudio(make([]float32, 4000), 16000))

	audio := clone.Audio()
	audio.Samples[0] = 1
	assert.Equal(t, float32(0.1), clone.Audio().Samples[0])
	clone.Reset()
	assert.False(t, clone.IsReady())
	assert.Empty(t, clone.Audio().Samples)
	assert.Zero(t, clone.RecordedSeconds())
	assert.Zero(t, clone.SpeechSeconds())
}

func TestVoiceCloneIgnoresEmptyAndInvalidRate(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{handle: 42}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, synthesizer.Close()) })
	clone, err := synthesizer.StartVoiceClone()
	require.NoError(t, err)
	require.NoError(t, clone.AddAudio(nil, 16000))
	require.NoError(t, clone.AddAudio([]float32{0.1}, 0))
	assert.Zero(t, clone.RecordedSeconds())
	assert.Empty(t, bindings.clipAudio)
}

func TestVoiceClonePropagatesSearchAndOwnerErrors(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{
		handle: 42, clipCode: rawErrorInvalidHandle, errorMessage: "Invalid handle",
	}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	clone, err := synthesizer.StartVoiceClone()
	require.NoError(t, err)
	err = clone.AddAudio(make([]float32, 4000), 16000)
	require.ErrorIs(t, err, ErrInvalidHandle)

	require.NoError(t, synthesizer.Close())
	err = clone.AddAudio(make([]float32, 4000), 16000)
	require.ErrorIs(t, err, ErrClosed)
	_, err = synthesizer.StartVoiceClone()
	require.ErrorIs(t, err, ErrClosed)
	var nilSynthesizer *TextToSpeech
	_, err = nilSynthesizer.StartVoiceClone()
	require.ErrorIs(t, err, ErrClosed)
	var nilClone *VoiceClone
	require.ErrorIs(t, nilClone.AddAudio([]float32{0}, 16000), ErrClosed)
}

func TestVoiceCloneConcurrentAudioIsRaceFree(t *testing.T) {
	bindings := &fakeTextToSpeechBindings{handle: 42}
	synthesizer, err := newTextToSpeechFromFiles(bindings, "en_us", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, synthesizer.Close()) })
	clone, err := synthesizer.StartVoiceClone()
	require.NoError(t, err)
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_ = clone.AddAudio(make([]float32, 1000), 16000)
		}()
	}
	wait.Wait()
	assert.Equal(t, 1.0, clone.RecordedSeconds())
}
