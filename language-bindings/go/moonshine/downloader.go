package moonshine

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DownloadProgress reports aggregate and current-file progress.
type DownloadProgress struct {
	Fraction        float64
	RelativePath    string
	FileIndex       int
	TotalFiles      int
	BytesDownloaded int64
	BytesTotal      int64
}

// Downloader installs model assets described by native manifests.
type Downloader struct {
	Client *http.Client
}

// NewDownloader creates a downloader. A nil client uses http.DefaultClient.
func NewDownloader(client *http.Client) *Downloader {
	return &Downloader{Client: client}
}

type resolvedDownload struct {
	file DownloadFile
	url  *url.URL
	rel  string
}

// Ensure downloads every missing or invalid file beneath root. Completed files
// are atomically renamed from a resumable .part file.
func (d *Downloader) Ensure(
	ctx context.Context,
	root string,
	manifest DownloadManifest,
	onProgress func(DownloadProgress),
) error {
	files, err := resolveDownloads(manifest)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("manifest contains no files: %w", ErrInvalidManifest)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve cache root: %w: %w", ErrAssetDownload, err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create cache root %q: %w: %w", root, ErrAssetDownload, err)
	}

	tracker := newDownloadTracker(files, onProgress)
	for index, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		destination, err := destinationUnderRoot(root, file.rel)
		if err != nil {
			return err
		}
		valid, err := validAsset(destination, file.file)
		if err != nil {
			return err
		}
		tracker.start(index, file, valid)
		if valid {
			tracker.finish(file, true)
			continue
		}
		if err := d.download(ctx, destination, file, tracker); err != nil {
			return err
		}
		tracker.finish(file, false)
	}
	return nil
}

// Present reports whether every manifest file exists and passes its declared
// size and checksum checks.
func (d *Downloader) Present(root string, manifest DownloadManifest) (bool, error) {
	files, err := resolveDownloads(manifest)
	if err != nil {
		return false, err
	}
	if len(files) == 0 {
		return false, nil
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return false, fmt.Errorf("resolve cache root: %w: %w", ErrAssetDownload, err)
	}
	for _, file := range files {
		destination, err := destinationUnderRoot(root, file.rel)
		if err != nil {
			return false, err
		}
		valid, err := validAsset(destination, file.file)
		if err != nil {
			return false, err
		}
		if !valid {
			return false, nil
		}
	}
	return true, nil
}

func resolveDownloads(manifest DownloadManifest) ([]resolvedDownload, error) {
	var resolved []resolvedDownload
	seen := make(map[string]struct{})
	for _, group := range manifest.Groups {
		for _, file := range group.Files {
			if !safeRelativeAssetPath(file.Name) {
				return nil, fmt.Errorf("unsafe asset path %q: %w", file.Name, ErrInvalidManifest)
			}
			if _, exists := seen[file.Name]; exists {
				return nil, fmt.Errorf("duplicate asset path %q: %w", file.Name, ErrInvalidManifest)
			}
			seen[file.Name] = struct{}{}
			address := file.URL
			if address == "" && group.BaseURL != "" {
				address = strings.TrimRight(group.BaseURL, "/") + "/" + file.Name
			}
			parsed, err := url.Parse(address)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
				return nil, fmt.Errorf("invalid URL %q for %q: %w", address, file.Name, ErrInvalidManifest)
			}
			if file.Checksum != "" && file.ChecksumType != "crc32c" {
				return nil, fmt.Errorf("unsupported checksum type %q for %q: %w", file.ChecksumType, file.Name, ErrInvalidManifest)
			}
			if file.Size != nil && *file.Size > uint64(^uint64(0)>>1) {
				return nil, fmt.Errorf("asset size overflows int64 for %q: %w", file.Name, ErrInvalidManifest)
			}
			resolved = append(resolved, resolvedDownload{file: file, url: parsed, rel: file.Name})
		}
	}
	return resolved, nil
}

func safeRelativeAssetPath(name string) bool {
	if name == "" || strings.ContainsRune(name, '\x00') || strings.Contains(name, `\`) {
		return false
	}
	local := filepath.FromSlash(name)
	return filepath.IsLocal(local) && filepath.Clean(local) == local
}

func destinationUnderRoot(root, relative string) (string, error) {
	current := root
	parts := strings.Split(filepath.FromSlash(relative), string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("inspect cache path %q: %w: %w", relative, ErrAssetDownload, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("cache path %q crosses a symlink: %w", relative, ErrInvalidManifest)
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", fmt.Errorf("cache parent for %q is not a directory: %w", relative, ErrAssetDownload)
		}
	}
	return current, nil
}

func (d *Downloader) download(
	ctx context.Context,
	destination string,
	file resolvedDownload,
	tracker *downloadTracker,
) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create directory for %q: %w: %w", file.rel, ErrAssetDownload, err)
	}
	part := destination + ".part"
	existing := int64(0)
	if info, err := os.Lstat(part); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("partial asset %q is not a regular file: %w", file.rel, ErrAssetDownload)
		}
		existing = info.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect partial file %q: %w: %w", file.rel, ErrAssetDownload, err)
	}
	if file.file.Size != nil && existing >= int64(*file.file.Size) {
		if err := os.Remove(part); err != nil {
			return fmt.Errorf("discard invalid partial file %q: %w: %w", file.rel, ErrAssetDownload, err)
		}
		existing = 0
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, file.url.String(), nil)
	if err != nil {
		return fmt.Errorf("create request for %q: %w: %w", file.rel, ErrAssetDownload, err)
	}
	if existing > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", existing))
	}
	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download %q: %w: %w", file.rel, ErrAssetDownload, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("download %q returned HTTP %d: %w", file.rel, response.StatusCode, ErrAssetDownload)
	}

	resume := existing > 0 && response.StatusCode == http.StatusPartialContent && validContentRange(response.Header.Get("Content-Range"), existing)
	flags := os.O_CREATE | os.O_WRONLY
	if resume {
		flags |= os.O_APPEND
	} else {
		existing = 0
		flags |= os.O_TRUNC
	}
	handle, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return fmt.Errorf("open partial file %q: %w: %w", file.rel, ErrAssetDownload, err)
	}
	written, copyErr := io.Copy(handle, io.TeeReader(response.Body, tracker.writer(file, existing)))
	closeErr := handle.Close()
	if copyErr != nil {
		return fmt.Errorf("write partial file %q: %w: %w", file.rel, ErrAssetDownload, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close partial file %q: %w: %w", file.rel, ErrAssetDownload, closeErr)
	}
	_ = written

	valid, err := validAsset(part, file.file)
	if err != nil {
		_ = os.Remove(part)
		return err
	}
	if !valid {
		_ = os.Remove(part)
		return fmt.Errorf("downloaded asset %q failed declared checks: %w", file.rel, ErrAssetIntegrity)
	}
	if err := os.Rename(part, destination); err != nil {
		if removeErr := os.Remove(destination); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("replace asset %q: %w: %w", file.rel, ErrAssetDownload, removeErr)
		}
		if err := os.Rename(part, destination); err != nil {
			return fmt.Errorf("install asset %q: %w: %w", file.rel, ErrAssetDownload, err)
		}
	}
	return nil
}

func validContentRange(value string, start int64) bool {
	prefix := "bytes " + strconv.FormatInt(start, 10) + "-"
	return strings.HasPrefix(value, prefix)
}

func validAsset(path string, file DownloadFile) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect asset %q: %w: %w", file.Name, ErrAssetDownload, err)
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	if file.Size != nil && info.Size() != int64(*file.Size) {
		return false, nil
	}
	if file.Checksum != "" {
		actual, err := crc32cBase64(path)
		if err != nil {
			return false, fmt.Errorf("checksum asset %q: %w: %w", file.Name, ErrAssetDownload, err)
		}
		if actual != file.Checksum {
			return false, nil
		}
	}
	return true, nil
}

func crc32cBase64(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := crc32.New(crc32.MakeTable(crc32.Castagnoli))
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	buffer := make([]byte, 4)
	binary.BigEndian.PutUint32(buffer, hash.Sum32())
	return base64.StdEncoding.EncodeToString(buffer), nil
}

type downloadTracker struct {
	callback     func(DownloadProgress)
	totalFiles   int
	totalBytes   int64
	doneFiles    int
	doneBytes    int64
	currentIndex int
	currentPath  string
	currentBytes int64
	currentTotal int64
	lastFraction float64
}

func newDownloadTracker(files []resolvedDownload, callback func(DownloadProgress)) *downloadTracker {
	total := int64(0)
	for _, file := range files {
		if file.file.Size == nil {
			total = 0
			break
		}
		total += int64(*file.file.Size)
	}
	return &downloadTracker{callback: callback, totalFiles: len(files), totalBytes: total, lastFraction: -1}
}

func (t *downloadTracker) start(index int, file resolvedDownload, cached bool) {
	t.currentIndex = index + 1
	t.currentPath = file.rel
	t.currentBytes = 0
	t.currentTotal = -1
	if file.file.Size != nil {
		t.currentTotal = int64(*file.file.Size)
	}
	_ = cached
	t.emit(true)
}

func (t *downloadTracker) writer(file resolvedDownload, existing int64) io.Writer {
	t.currentBytes = existing
	t.emit(true)
	return progressWriter{write: func(count int) {
		t.currentBytes += int64(count)
		t.emit(false)
	}}
}

func (t *downloadTracker) finish(file resolvedDownload, cached bool) {
	if t.totalBytes > 0 {
		t.currentBytes = int64(*file.file.Size)
		t.emit(true)
		t.doneBytes += t.currentBytes
		t.doneFiles++
		t.currentBytes = 0
		return
	}
	t.doneFiles++
	if file.file.Size != nil {
		t.currentBytes = int64(*file.file.Size)
	} else if cached {
		t.currentBytes = 0
	}
	t.emit(true)
	t.currentBytes = 0
}

func (t *downloadTracker) emit(force bool) {
	if t.callback == nil {
		return
	}
	fraction := float64(t.doneFiles) / float64(max(t.totalFiles, 1))
	if t.totalBytes > 0 {
		current := min(t.currentBytes, max(t.currentTotal, 0))
		fraction = float64(t.doneBytes+current) / float64(t.totalBytes)
	}
	fraction = min(max(fraction, t.lastFraction), 1)
	if !force && fraction-t.lastFraction < 0.002 {
		return
	}
	t.lastFraction = fraction
	t.callback(DownloadProgress{
		Fraction: fraction, RelativePath: t.currentPath,
		FileIndex: t.currentIndex, TotalFiles: t.totalFiles,
		BytesDownloaded: t.currentBytes, BytesTotal: t.currentTotal,
	})
}

type progressWriter struct{ write func(int) }

func (w progressWriter) Write(buffer []byte) (int, error) {
	w.write(len(buffer))
	return len(buffer), nil
}
