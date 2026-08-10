package moonshine

import (
	"encoding/json"
	"fmt"
	"strings"
	"unsafe"

	"github.com/moonshine-ai/moonshine/language-bindings/go/raw"
)

// DownloadManifest describes groups of model files published by Moonshine.
type DownloadManifest struct {
	Groups []DownloadGroup `json:"groups"`
}

// DownloadGroup contains files sharing one remote base URL.
type DownloadGroup struct {
	BaseURL string         `json:"base_url"`
	Files   []DownloadFile `json:"files"`
}

// DownloadFile describes one model asset and its optional integrity metadata.
type DownloadFile struct {
	Name         string  `json:"name"`
	URL          string  `json:"url"`
	Size         *uint64 `json:"size"`
	Checksum     string  `json:"checksum"`
	ChecksumType string  `json:"checksum_type"`
}

type manifestBindings interface {
	sttDependencies(language string, options []Option, output **byte) int32
	diarizationDependencies(output **byte) int32
	sttCatalog(output **byte) int32
	freeBuffer(pointer *byte)
	errorToString(code int32) string
}

type rawManifestBindings struct{}

func (rawManifestBindings) sttDependencies(language string, options []Option, output **byte) int32 {
	converted := rawOptions(options)
	return raw.MoonshineGetSttDependencies(language, converted, uint64(len(converted)), output)
}

func (rawManifestBindings) diarizationDependencies(output **byte) int32 {
	return raw.MoonshineGetDiarizationDependencies(output)
}

func (rawManifestBindings) sttCatalog(output **byte) int32 {
	return raw.MoonshineGetSttCatalog(output)
}

func (rawManifestBindings) freeBuffer(pointer *byte) {
	raw.MoonshineFreeBuffer(unsafe.Pointer(pointer))
}

func (rawManifestBindings) errorToString(code int32) string {
	return copyCString(raw.MoonshineErrorToString(code))
}

// STTDependencies returns the native download manifest for a language and
// transcriber option set.
func STTDependencies(language string, options ...Option) (DownloadManifest, error) {
	return sttDependencies(rawManifestBindings{}, language, options...)
}

func sttDependencies(bindings manifestBindings, language string, options ...Option) (DownloadManifest, error) {
	if language == "" || strings.IndexByte(language, 0) >= 0 {
		return DownloadManifest{}, fmt.Errorf("invalid STT language %q: %w", language, ErrInvalidArgument)
	}
	if err := validateOptions(options); err != nil {
		return DownloadManifest{}, err
	}
	return loadManifest(bindings, "get STT dependencies", func(output **byte) int32 {
		return bindings.sttDependencies(language, options, output)
	})
}

// DiarizationDependencies returns the native download manifest for Moonshine's
// speaker diarization models.
func DiarizationDependencies() (DownloadManifest, error) {
	return diarizationDependencies(rawManifestBindings{})
}

func diarizationDependencies(bindings manifestBindings) (DownloadManifest, error) {
	return loadManifest(bindings, "get diarization dependencies", bindings.diarizationDependencies)
}

func loadManifest(
	bindings manifestBindings,
	operation string,
	call func(output **byte) int32,
) (DownloadManifest, error) {
	var output *byte
	code := call(&output)
	if output != nil {
		defer bindings.freeBuffer(output)
	}
	if code < 0 {
		return DownloadManifest{}, fmt.Errorf(
			"moonshine: %s: %w",
			operation,
			nativeError(code, bindings.errorToString(code)),
		)
	}
	if output == nil {
		return DownloadManifest{}, fmt.Errorf("moonshine: %s returned nil: %w", operation, ErrInvalidNativeOutput)
	}

	var manifest DownloadManifest
	if err := json.Unmarshal([]byte(copyCString(output)), &manifest); err != nil {
		return DownloadManifest{}, fmt.Errorf("moonshine: decode %s: %w: %w", operation, ErrInvalidNativeOutput, err)
	}
	return manifest, nil
}
