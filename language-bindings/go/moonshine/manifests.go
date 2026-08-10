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
	g2pDependencies(languages string, options []Option, output **byte) int32
	ttsDependencies(languages string, options []Option, output **byte) int32
	sttDependencies(language string, options []Option, output **byte) int32
	diarizationDependencies(output **byte) int32
	sttCatalog(output **byte) int32
	embeddingDependencies(model string, options []Option, output **byte) int32
	embeddingCatalog(output **byte) int32
	freeBuffer(pointer *byte)
	errorToString(code int32) string
}

type rawManifestBindings struct{}

func (rawManifestBindings) g2pDependencies(languages string, options []Option, output **byte) int32 {
	converted := rawOptions(options)
	return raw.MoonshineGetG2pDependencies(languages, converted, uint64(len(converted)), output)
}

func (rawManifestBindings) ttsDependencies(languages string, options []Option, output **byte) int32 {
	converted := rawOptions(options)
	return raw.MoonshineGetTtsDependencies(languages, converted, uint64(len(converted)), output)
}

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

func (rawManifestBindings) embeddingDependencies(model string, options []Option, output **byte) int32 {
	converted := rawOptions(options)
	return raw.MoonshineGetEmbeddingDependencies(model, converted, uint64(len(converted)), output)
}

func (rawManifestBindings) embeddingCatalog(output **byte) int32 {
	return raw.MoonshineGetEmbeddingCatalog(output)
}

func (rawManifestBindings) freeBuffer(pointer *byte) {
	raw.MoonshineFreeBuffer(unsafe.Pointer(pointer))
}

func (rawManifestBindings) errorToString(code int32) string {
	return copyCString(raw.MoonshineErrorToString(code))
}

// G2PDependencies returns canonical asset keys required by the requested
// languages. An empty language slice requests every known language.
func G2PDependencies(languages []string, options ...Option) ([]string, error) {
	return g2pDependencies(rawManifestBindings{}, languages, options...)
}

func g2pDependencies(bindings manifestBindings, languages []string, options ...Option) ([]string, error) {
	joined, err := joinManifestLanguages("G2P", languages, options)
	if err != nil {
		return nil, err
	}
	var output *byte
	code := bindings.g2pDependencies(joined, options, &output)
	if output != nil {
		defer bindings.freeBuffer(output)
	}
	if code < 0 {
		return nil, fmt.Errorf(
			"moonshine: get G2P dependencies: %w",
			nativeError(code, bindings.errorToString(code)),
		)
	}
	if output == nil {
		return nil, fmt.Errorf("moonshine: get G2P dependencies returned nil: %w", ErrInvalidNativeOutput)
	}
	value := copyCString(output)
	if value == "" {
		return []string{}, nil
	}
	keys := strings.Split(value, ",")
	for _, key := range keys {
		if key == "" {
			return nil, fmt.Errorf("moonshine: get G2P dependencies returned an empty key: %w", ErrInvalidNativeOutput)
		}
	}
	return keys, nil
}

// TTSDependencies returns the native download manifest for the requested
// languages. An empty language slice requests every known language.
func TTSDependencies(languages []string, options ...Option) (DownloadManifest, error) {
	return ttsDependencies(rawManifestBindings{}, languages, options...)
}

func ttsDependencies(
	bindings manifestBindings, languages []string, options ...Option,
) (DownloadManifest, error) {
	joined, err := joinManifestLanguages("TTS", languages, options)
	if err != nil {
		return DownloadManifest{}, err
	}
	return loadManifest(bindings, "get TTS dependencies", func(output **byte) int32 {
		return bindings.ttsDependencies(joined, options, output)
	})
}

func joinManifestLanguages(kind string, languages []string, options []Option) (string, error) {
	if err := validateOptions(options); err != nil {
		return "", err
	}
	for _, language := range languages {
		if language == "" || strings.IndexByte(language, 0) >= 0 || strings.Contains(language, ",") {
			return "", fmt.Errorf("invalid %s language %q: %w", kind, language, ErrInvalidArgument)
		}
	}
	return strings.Join(languages, ","), nil
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
