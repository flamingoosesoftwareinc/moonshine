//go:build tts_integration

package moonshine

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeTextPhonemeAndMemorySynthesisPaths(t *testing.T) {
	root := ttsTestAssetPath(t)
	createOptions := []Option{
		{Name: "g2p_root", Value: root},
		{Name: "voice", Value: "piper_en_US-amy-low"},
	}
	manifest, err := TTSDependencies([]string{"en_us"}, createOptions...)
	require.NoError(t, err)
	assert.NotEmpty(t, manifest.Groups)
	voices, err := TTSVoices([]string{"en_us"}, createOptions...)
	require.NoError(t, err)
	assert.Contains(t, voices["en_us"], Voice{ID: "piper_en_US-amy-low", State: VoiceStateFound})
	synthesizer, err := NewTextToSpeechFromFiles("en_us", nil, createOptions...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, synthesizer.Close()) })
	phonemizer, err := NewPhonemizerFromFiles(
		"en_us", nil, Option{Name: "g2p_root", Value: root},
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, phonemizer.Close()) })

	const text = "Hello world, this is a phoneme test."
	ipa, err := phonemizer.Phonemes(text)
	require.NoError(t, err)
	fromText, err := synthesizer.Synthesize(text)
	require.NoError(t, err)
	fromPhonemes, err := synthesizer.SynthesizePhonemes(ipa)
	require.NoError(t, err)
	faster, err := synthesizer.SynthesizePhonemes(ipa, Option{Name: "speed", Value: "2.0"})
	require.NoError(t, err)

	assert.Equal(t, 24000, fromText.SampleRate)
	assert.Equal(t, fromText.SampleRate, fromPhonemes.SampleRate)
	assert.Greater(t, len(fromText.Samples), 2000)
	assert.Greater(t, len(fromPhonemes.Samples), 2000)
	assert.Less(t, len(faster.Samples), len(fromPhonemes.Samples))
	clip, err := synthesizer.ExtractSpeechClip(
		fromText.Samples,
		fromText.SampleRate,
		Option{Name: "clip_duration_seconds", Value: "1"},
		Option{Name: "minimum_speech_seconds", Value: "0"},
	)
	require.NoError(t, err)
	assert.True(t, clip.Complete)
	assert.Equal(t, 16000, clip.Audio.SampleRate)
	assert.NotEmpty(t, clip.Audio.Samples)
	clone, err := synthesizer.StartVoiceClone(
		Option{Name: "clip_duration_seconds", Value: "1"},
		Option{Name: "minimum_speech_seconds", Value: "0"},
	)
	require.NoError(t, err)
	readyCount := 0
	clone.OnReady(func() { readyCount++ })
	chunkSize := fromText.SampleRate / 4
	for start := 0; start < len(fromText.Samples) && !clone.IsReady(); start += chunkSize {
		end := min(start+chunkSize, len(fromText.Samples))
		require.NoError(t, clone.AddAudio(fromText.Samples[start:end], fromText.SampleRate))
	}
	assert.True(t, clone.IsReady())
	assert.Equal(t, VoiceCloneSampleRate, clone.Audio().SampleRate)
	assert.NotEmpty(t, clone.Audio().Samples)
	assert.Equal(t, 1, readyCount)

	memoryFiles := readG2PFiles(t, root)
	memorySynthesizer, err := NewTextToSpeechFromMemory("en_us", memoryFiles, createOptions...)
	require.NoError(t, err)
	memoryAudio, err := memorySynthesizer.Synthesize("Hello")
	require.NoError(t, err)
	require.NoError(t, memorySynthesizer.Close())
	assert.Equal(t, 24000, memoryAudio.SampleRate)
	assert.Greater(t, len(memoryAudio.Samples), 2000)
}

func readG2PFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	keys, err := G2PDependencies([]string{"en_us"}, Option{Name: "g2p_root", Value: root})
	require.NoError(t, err)
	files := make(map[string][]byte, len(keys))
	for _, key := range keys {
		files[key], err = os.ReadFile(filepath.Join(root, filepath.FromSlash(key)))
		require.NoError(t, err, key)
	}
	return files
}

func ttsTestAssetPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(
		filepath.Dir(filename), "../../..", "core", "moonshine-tts", "data",
	))
}
