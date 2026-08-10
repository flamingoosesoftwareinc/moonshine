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

func TestNativeStreamLifecycle(t *testing.T) {
	transcriber, err := NewTranscriber(tinyEnglishModelPath(t), ModelArchTiny)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, transcriber.Close()) })

	stream, err := transcriber.NewStream()
	require.NoError(t, err)
	require.NoError(t, stream.Start())
	require.NoError(t, stream.Stop())
	require.NoError(t, stream.Close())
	require.NoError(t, stream.Close())
}

func TestNativeStreamAcceptsSpellingMode(t *testing.T) {
	transcriber, err := NewTranscriber(tinyEnglishModelPath(t), ModelArchTiny)
	require.NoError(t, err)

	stream, err := transcriber.NewStream(FlagSpellingMode)
	require.NoError(t, err)
	require.NoError(t, stream.Start())
	require.NoError(t, stream.Stop())

	// The parent owns any stream left open and must release it first.
	require.NoError(t, transcriber.Close())
	require.ErrorIs(t, stream.Start(), ErrClosed)
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

func TestNativeStreamTranscribesWAVFixtures(t *testing.T) {
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
			stream, err := transcriber.NewStream()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, stream.Close()) })
			require.NoError(t, stream.Start())

			wav, err := testasset.LoadWAV(filepath.Join(testAssetsPath(t), test.filename))
			require.NoError(t, err)
			chunkSize := wav.SampleRate / 10
			var transcript Transcript
			chunksSinceUpdate := 0
			for offset := 0; offset < len(wav.Samples); offset += chunkSize {
				end := min(offset+chunkSize, len(wav.Samples))
				require.NoError(t, stream.AddAudio(wav.Samples[offset:end], wav.SampleRate))
				chunksSinceUpdate++
				if chunksSinceUpdate == 5 {
					transcript, err = stream.Transcript()
					require.NoError(t, err)
					chunksSinceUpdate = 0
				}
			}
			require.NoError(t, stream.Stop())

			transcript, err = stream.Transcript(FlagForceUpdate)
			require.NoError(t, err)
			require.NotEmpty(t, transcript.Lines)
			allText := strings.ToLower(transcriptText(transcript))
			for _, phrase := range test.phrases {
				assert.Contains(t, allText, phrase)
			}
			for _, line := range transcript.Lines {
				assert.NotEmpty(t, line.Text)
				assert.Positive(t, line.Duration)
			}
		})
	}
}

func TestNativeStreamManualSnapshotsAndEmptyAudio(t *testing.T) {
	transcriber, err := NewTranscriber(tinyEnglishModelPath(t), ModelArchTiny)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, transcriber.Close()) })
	stream, err := transcriber.NewStream()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stream.Close()) })
	require.NoError(t, stream.Start())
	require.NoError(t, stream.AddAudio(nil, 16000))

	first, err := stream.Transcript()
	require.NoError(t, err)
	second, err := stream.Transcript(FlagForceUpdate)
	require.NoError(t, err)
	require.NoError(t, stream.Stop())

	assert.Empty(t, first.Lines)
	assert.Empty(t, second.Lines)
}

func TestNativeSTTDependencies(t *testing.T) {
	manifest, err := STTDependencies(
		"en",
		Option{Name: "model_arch", Value: "0"},
		Option{Name: "word_timestamps", Value: "true"},
	)

	require.NoError(t, err)
	require.NotEmpty(t, manifest.Groups)
	var names []string
	for _, group := range manifest.Groups {
		assert.NotEmpty(t, group.BaseURL)
		for _, file := range group.Files {
			assert.NotEmpty(t, file.Name)
			assert.NotEmpty(t, file.URL)
			names = append(names, file.Name)
		}
	}
	assert.Contains(t, names, "encoder_model.ort")
	assert.Contains(t, names, "decoder_model_merged.ort")
	assert.Contains(t, names, "decoder_with_attention.ort")
	assert.Contains(t, names, "tokenizer.bin")
}

func TestNativeSTTDependenciesRejectUnknownLanguage(t *testing.T) {
	_, err := STTDependencies("not-a-moonshine-language")
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestNativeDiarizationDependencies(t *testing.T) {
	manifest, err := DiarizationDependencies()

	require.NoError(t, err)
	require.Len(t, manifest.Groups, 1)
	require.Len(t, manifest.Groups[0].Files, 2)
	assert.Equal(t, "segmentation.ort", manifest.Groups[0].Files[0].Name)
	assert.Equal(t, "embedding.ort", manifest.Groups[0].Files[1].Name)
	for _, file := range manifest.Groups[0].Files {
		assert.NotEmpty(t, file.URL)
		require.NotNil(t, file.Size)
		assert.Positive(t, *file.Size)
	}
}

func TestNativeSTTCatalog(t *testing.T) {
	catalog, err := STTCatalog()

	require.NoError(t, err)
	require.NotEmpty(t, catalog.Languages)
	var english *STTLanguage
	for index := range catalog.Languages {
		language := &catalog.Languages[index]
		assert.NotEmpty(t, language.Code)
		assert.NotEmpty(t, language.EnglishName)
		assert.NotEmpty(t, language.Models)
		if language.Code == "en" {
			english = language
		}
	}
	require.NotNil(t, english)
	require.NotEmpty(t, english.Models)
	assert.True(t, english.Models[0].IsDefault)
	for _, model := range english.Models {
		assert.NotEmpty(t, model.DownloadURL)
	}
}

func transcriptText(transcript Transcript) string {
	var text strings.Builder
	for _, line := range transcript.Lines {
		text.WriteString(line.Text)
		text.WriteByte(' ')
	}
	return text.String()
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
