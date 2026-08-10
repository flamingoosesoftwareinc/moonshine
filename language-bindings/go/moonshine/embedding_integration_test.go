//go:build embedding_integration

package moonshine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeEmbeddingFilesMemoryAndSimilarity(t *testing.T) {
	manifest, err := EmbeddingDependencies("embeddinggemma-300m", "q4")
	require.NoError(t, err)
	root := embeddingTestAssetPath(t)
	require.NoError(t, NewDownloader(nil).Ensure(context.Background(), root, manifest, nil))

	fileModel, err := NewEmbeddingModel(root, EmbeddingModelArchGemma300M, "q4")
	require.NoError(t, err)
	lights, lamps, garage := embeddingFixtureVectors(t, fileModel)
	require.NoError(t, fileModel.Close())
	_, err = fileModel.Embed("closed", "")
	require.ErrorIs(t, err, ErrClosed)

	files := make(map[string][]byte)
	for _, name := range []string{"model_q4.ort", "tokenizer.bin"} {
		files[name], err = os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
	}
	memoryModel, err := NewEmbeddingModelFromMemory(
		files, EmbeddingModelArchGemma300M, "q4",
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, memoryModel.Close()) })
	memoryLights, err := memoryModel.Embed("turn on the lights", "")
	require.NoError(t, err)

	assert.Len(t, lights, 768)
	assert.Equal(t, len(lights), len(lamps))
	assert.Equal(t, len(lights), len(garage))
	assert.InDelta(t, 1, similarity(t, memoryModel, lights, memoryLights), 0.0001)
	assert.Greater(t,
		similarity(t, memoryModel, lights, lamps),
		similarity(t, memoryModel, lights, garage),
	)
}

func embeddingFixtureVectors(t *testing.T, model *EmbeddingModel) ([]float32, []float32, []float32) {
	t.Helper()
	lights, err := model.Embed("turn on the lights", "")
	require.NoError(t, err)
	lamps, err := model.Embed("switch on the lamps", "")
	require.NoError(t, err)
	garage, err := model.Embed("close the garage door", "")
	require.NoError(t, err)
	assert.Greater(t, similarity(t, model, lights, lights), float32(0.99))
	return lights, lamps, garage
}

func similarity(t *testing.T, model *EmbeddingModel, a, b []float32) float32 {
	t.Helper()
	value, err := model.Similarity(a, b)
	require.NoError(t, err)
	return value
}

func embeddingTestAssetPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(
		filepath.Dir(filename), "../../..", "test-assets", "embeddinggemma-300m-ONNX",
	))
}
