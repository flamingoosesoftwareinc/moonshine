//go:build g2p_integration

package moonshine

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativePhonemizerFilesMemoryAndDependencies(t *testing.T) {
	root := g2pTestAssetPath(t)
	keys, err := G2PDependencies([]string{"en_us"}, Option{Name: "g2p_root", Value: root})
	require.NoError(t, err)
	require.NotEmpty(t, keys)

	fromFiles, err := NewPhonemizerFromFiles(
		"en_us", nil, Option{Name: "g2p_root", Value: root},
	)
	require.NoError(t, err)
	fileIPA, err := fromFiles.Phonemes("Hello world")
	require.NoError(t, err)
	require.NoError(t, fromFiles.Close())
	_, err = fromFiles.Phonemes("closed")
	require.ErrorIs(t, err, ErrClosed)

	files := make(map[string][]byte, len(keys))
	for _, key := range keys {
		files[key], err = os.ReadFile(filepath.Join(root, filepath.FromSlash(key)))
		require.NoError(t, err, key)
	}
	fromMemory, err := NewPhonemizerFromMemory("en_us", files)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, fromMemory.Close()) })
	memoryIPA, err := fromMemory.Phonemes("Hello world")
	require.NoError(t, err)
	emptyIPA, err := fromMemory.Phonemes("")
	require.NoError(t, err)

	assert.NotEmpty(t, fileIPA)
	assert.True(t, utf8.ValidString(fileIPA))
	assert.Equal(t, fileIPA, memoryIPA)
	assert.Empty(t, emptyIPA)
}

func g2pTestAssetPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(
		filepath.Dir(filename), "../../..", "core", "moonshine-tts", "data",
	))
}
