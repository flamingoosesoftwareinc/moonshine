package moonshine

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func downloadFile(name, address string, body []byte) DownloadFile {
	size := uint64(len(body))
	return DownloadFile{
		Name: name, URL: address, Size: &size,
		Checksum: crc32cBytes(body), ChecksumType: "crc32c",
	}
}

func crc32cBytes(body []byte) string {
	value := crc32.Checksum(body, crc32.MakeTable(crc32.Castagnoli))
	encoded := make([]byte, 4)
	binary.BigEndian.PutUint32(encoded, value)
	return base64.StdEncoding.EncodeToString(encoded)
}

func TestDownloaderInstallsVerifiedManifestAndReportsProgress(t *testing.T) {
	bodies := map[string][]byte{
		"/model.ort":   []byte("model bytes"),
		"/nested/data": []byte("nested bytes"),
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		body, ok := bodies[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	manifest := DownloadManifest{Groups: []DownloadGroup{{Files: []DownloadFile{
		downloadFile("model.ort", server.URL+"/model.ort", bodies["/model.ort"]),
		downloadFile("nested/data", server.URL+"/nested/data", bodies["/nested/data"]),
	}}}}
	root := t.TempDir()
	var progress []DownloadProgress
	downloader := NewDownloader(server.Client())

	err := downloader.Ensure(context.Background(), root, manifest, func(event DownloadProgress) {
		progress = append(progress, event)
	})

	require.NoError(t, err)
	assert.Equal(t, int32(2), requests.Load())
	for path, body := range bodies {
		contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(path, "/"))))
		require.NoError(t, err)
		assert.Equal(t, body, contents)
	}
	entries, err := filepath.Glob(filepath.Join(root, "**", "*.part"))
	require.NoError(t, err)
	assert.Empty(t, entries)
	require.NotEmpty(t, progress)
	assert.Zero(t, progress[0].Fraction)
	assert.Equal(t, 1.0, progress[len(progress)-1].Fraction)
	fractions := make([]float64, len(progress))
	for index, event := range progress {
		fractions[index] = event.Fraction
		assert.GreaterOrEqual(t, event.FileIndex, 1)
		assert.LessOrEqual(t, event.FileIndex, event.TotalFiles)
	}
	assert.True(t, slices.IsSorted(fractions))
	present, err := downloader.Present(root, manifest)
	require.NoError(t, err)
	assert.True(t, present)
}

func TestDownloaderSkipsVerifiedCachedFiles(t *testing.T) {
	body := []byte("already cached")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	manifest := DownloadManifest{Groups: []DownloadGroup{{Files: []DownloadFile{
		downloadFile("model.ort", server.URL+"/model.ort", body),
	}}}}
	root := t.TempDir()
	downloader := NewDownloader(server.Client())
	require.NoError(t, downloader.Ensure(context.Background(), root, manifest, nil))

	var progress []DownloadProgress
	require.NoError(t, downloader.Ensure(context.Background(), root, manifest, func(event DownloadProgress) {
		progress = append(progress, event)
	}))

	assert.Equal(t, int32(1), requests.Load())
	require.Len(t, progress, 2)
	assert.Zero(t, progress[0].Fraction)
	assert.Equal(t, 1.0, progress[1].Fraction)
}

func TestDownloaderResumesPartialFile(t *testing.T) {
	body := []byte("0123456789abcdef")
	prefix := body[:6]
	var rangeHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader = r.Header.Get("Range")
		w.Header().Set("Content-Range", "bytes 6-15/16")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body[6:])
	}))
	t.Cleanup(server.Close)
	manifest := DownloadManifest{Groups: []DownloadGroup{{Files: []DownloadFile{
		downloadFile("model.ort", server.URL+"/model.ort", body),
	}}}}
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "model.ort.part"), prefix, 0o644))

	err := NewDownloader(server.Client()).Ensure(context.Background(), root, manifest, nil)

	require.NoError(t, err)
	assert.Equal(t, "bytes=6-", rangeHeader)
	contents, err := os.ReadFile(filepath.Join(root, "model.ort"))
	require.NoError(t, err)
	assert.Equal(t, body, contents)
}

func TestDownloaderRestartsWhenServerIgnoresRange(t *testing.T) {
	body := []byte("complete response")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	manifest := DownloadManifest{Groups: []DownloadGroup{{Files: []DownloadFile{
		downloadFile("model.ort", server.URL+"/model.ort", body),
	}}}}
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "model.ort.part"), []byte("partial"), 0o644))

	err := NewDownloader(server.Client()).Ensure(context.Background(), root, manifest, nil)

	require.NoError(t, err)
	contents, err := os.ReadFile(filepath.Join(root, "model.ort"))
	require.NoError(t, err)
	assert.Equal(t, body, contents)
}

func TestDownloaderRejectsIntegrityFailureAndRemovesPartial(t *testing.T) {
	want := []byte("expected")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("corrupt!"))
	}))
	t.Cleanup(server.Close)
	manifest := DownloadManifest{Groups: []DownloadGroup{{Files: []DownloadFile{
		downloadFile("model.ort", server.URL+"/model.ort", want),
	}}}}
	root := t.TempDir()

	err := NewDownloader(server.Client()).Ensure(context.Background(), root, manifest, nil)

	require.ErrorIs(t, err, ErrAssetIntegrity)
	assert.NoFileExists(t, filepath.Join(root, "model.ort"))
	assert.NoFileExists(t, filepath.Join(root, "model.ort.part"))
}

func TestDownloaderMapsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	manifest := DownloadManifest{Groups: []DownloadGroup{{Files: []DownloadFile{{
		Name: "model.ort", URL: server.URL + "/missing",
	}}}}}

	err := NewDownloader(server.Client()).Ensure(context.Background(), t.TempDir(), manifest, nil)

	require.ErrorIs(t, err, ErrAssetDownload)
}

func TestDownloaderRejectsUnsafeManifestBeforeNetworkOrDiskWrites(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(server.Close)
	for _, name := range []string{"", "../escape", "/absolute", "a/../../escape", `windows\escape`, "a\x00b"} {
		t.Run(strconv.Quote(name), func(t *testing.T) {
			manifest := DownloadManifest{Groups: []DownloadGroup{{Files: []DownloadFile{{
				Name: name, URL: server.URL + "/asset",
			}}}}}
			root := filepath.Join(t.TempDir(), "cache")

			err := NewDownloader(server.Client()).Ensure(context.Background(), root, manifest, nil)

			require.ErrorIs(t, err, ErrInvalidManifest)
			assert.NoDirExists(t, root)
		})
	}
	assert.Zero(t, requests.Load())
}

func TestDownloaderRejectsDuplicatePathsAndUnsupportedChecksum(t *testing.T) {
	tests := []DownloadManifest{
		{Groups: []DownloadGroup{{BaseURL: "https://example.test", Files: []DownloadFile{
			{Name: "same"}, {Name: "same"},
		}}}},
		{Groups: []DownloadGroup{{Files: []DownloadFile{{
			Name: "asset", URL: "https://example.test/asset", Checksum: "abc", ChecksumType: "sha256",
		}}}}},
	}
	for _, manifest := range tests {
		_, err := NewDownloader(nil).Present(t.TempDir(), manifest)
		require.ErrorIs(t, err, ErrInvalidManifest)
	}
}

func TestDownloaderRejectsSymlinkedCacheComponents(t *testing.T) {
	outside := t.TempDir()
	root := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "nested")))
	manifest := DownloadManifest{Groups: []DownloadGroup{{Files: []DownloadFile{{
		Name: "nested/model.ort", URL: "https://example.test/model.ort",
	}}}}}

	err := NewDownloader(nil).Ensure(context.Background(), root, manifest, nil)

	require.ErrorIs(t, err, ErrInvalidManifest)
	assert.NoFileExists(t, filepath.Join(outside, "model.ort"))
}

func TestDownloaderRejectsSymlinkedPartialFile(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(outside, []byte("do not overwrite"), 0o644))
	root := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "model.ort.part")))
	manifest := DownloadManifest{Groups: []DownloadGroup{{Files: []DownloadFile{{
		Name: "model.ort", URL: "https://example.test/model.ort",
	}}}}}

	err := NewDownloader(nil).Ensure(context.Background(), root, manifest, nil)

	require.ErrorIs(t, err, ErrAssetDownload)
	contents, readErr := os.ReadFile(outside)
	require.NoError(t, readErr)
	assert.Equal(t, []byte("do not overwrite"), contents)
}
