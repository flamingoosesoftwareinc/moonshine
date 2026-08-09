//go:build integration

package moonshine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/moonshine-ai/moonshine/language-bindings/go/internal/testasset"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeTranscriberLifecycleFromFiles(t *testing.T) {
	transcriber, err := NewTranscriber(tinyEnglishModelPath(t), ModelArchTiny)
	require.NoError(t, err)
	require.NotNil(t, transcriber)
	t.Cleanup(func() {
		require.NoError(t, transcriber.Close())
	})
}

func TestNativeTranscriberLifecycleFromMemory(t *testing.T) {
	modelPath := tinyEnglishModelPath(t)
	files := map[string][]byte{}
	for _, filename := range []string{
		"encoder_model.ort",
		"decoder_model_merged.ort",
		"tokenizer.bin",
	} {
		contents, err := os.ReadFile(filepath.Join(modelPath, filename))
		require.NoError(t, err)
		files[filename] = contents
	}

	transcriber, err := NewTranscriberFromMemory(files, ModelArchTiny)
	require.NoError(t, err)
	require.NotNil(t, transcriber)
	t.Cleanup(func() {
		require.NoError(t, transcriber.Close())
	})
}

func TestNativeTranscriberRejectsMissingModelPath(t *testing.T) {
	transcriber, err := NewTranscriber(t.TempDir(), ModelArchTiny)

	require.ErrorIs(t, err, ErrUnknown)
	assert.Nil(t, transcriber)
}

func TestNativeTranscriberEmptyAudio(t *testing.T) {
	transcriber, err := NewTranscriber(tinyEnglishModelPath(t), ModelArchTiny)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, transcriber.Close()) })

	transcript, err := transcriber.Transcribe(nil, 16000)

	require.NoError(t, err)
	assert.Empty(t, transcript.Lines)
}

func TestNativeTranscriberTranscribesWAVFixtures(t *testing.T) {
	tests := []struct {
		filename string
		phrases  []string
	}{
		{filename: "beckett.wav"},
		{filename: "two_cities.wav", phrases: []string{"best of times", "worst of times"}},
	}

	for _, test := range tests {
		t.Run(test.filename, func(t *testing.T) {
			transcriber, err := NewTranscriber(tinyEnglishModelPath(t), ModelArchTiny)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, transcriber.Close()) })

			wav, err := testasset.LoadWAV(filepath.Join(testAssetsPath(t), test.filename))
			require.NoError(t, err)
			require.NotEmpty(t, wav.Samples)

			transcript, err := transcriber.Transcribe(wav.Samples, wav.SampleRate)
			require.NoError(t, err)
			require.NotEmpty(t, transcript.Lines)

			var allText strings.Builder
			for _, line := range transcript.Lines {
				assert.NotEmpty(t, line.Text)
				assert.Positive(t, line.Duration)
				assert.True(t, line.IsComplete)
				assert.True(t, line.IsNew)
				assert.True(t, line.IsUpdated)
				assert.True(t, line.HasTextChanged)
				allText.WriteString(strings.ToLower(line.Text))
				allText.WriteByte(' ')
			}
			for _, phrase := range test.phrases {
				assert.Contains(t, allText.String(), phrase)
			}
		})
	}
}

func tinyEnglishModelPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(testAssetsPath(t), "tiny-en")
}

func testAssetsPath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	path := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../..", "test-assets"))
	require.DirExists(t, path)
	return path
}
