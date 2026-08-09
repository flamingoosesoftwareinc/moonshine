//go:build integration

package moonshine

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

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

func tinyEnglishModelPath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	path := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../..", "test-assets", "tiny-en"))
	require.DirExists(t, path)
	return path
}
