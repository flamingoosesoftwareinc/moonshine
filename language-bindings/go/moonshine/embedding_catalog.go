package moonshine

import (
	"encoding/json"
	"fmt"
	"strings"
)

// EmbeddingCatalogData contains every text embedding model published by the
// linked Moonshine library.
type EmbeddingCatalogData struct {
	Models []EmbeddingCatalogModel `json:"models"`
}

// EmbeddingCatalogModel describes one text embedding model and its variants.
type EmbeddingCatalogModel struct {
	Name           string   `json:"name"`
	EnglishName    string   `json:"english_name"`
	DownloadURL    string   `json:"download_url"`
	Variants       []string `json:"variants"`
	DefaultVariant string   `json:"default_variant"`
}

// EmbeddingDependencies returns a verified-download manifest for an embedding
// model. An empty model selects the native default. variant is translated to
// the native variant option when non-empty.
func EmbeddingDependencies(model, variant string, options ...Option) (DownloadManifest, error) {
	return embeddingDependencies(rawManifestBindings{}, model, variant, options...)
}

func embeddingDependencies(
	bindings manifestBindings,
	model, variant string,
	options ...Option,
) (DownloadManifest, error) {
	if strings.IndexByte(model, 0) >= 0 || strings.IndexByte(variant, 0) >= 0 {
		return DownloadManifest{}, fmt.Errorf("embedding model or variant contains a NUL: %w", ErrInvalidArgument)
	}
	if err := validateOptions(options); err != nil {
		return DownloadManifest{}, err
	}
	allOptions := append([]Option(nil), options...)
	if variant != "" {
		allOptions = append(allOptions, Option{Name: "variant", Value: variant})
	}
	return loadManifest(bindings, "get embedding dependencies", func(output **byte) int32 {
		return bindings.embeddingDependencies(model, allOptions, output)
	})
}

// EmbeddingCatalog returns the text embedding model catalog.
func EmbeddingCatalog() (EmbeddingCatalogData, error) {
	return embeddingCatalog(rawManifestBindings{})
}

func embeddingCatalog(bindings manifestBindings) (EmbeddingCatalogData, error) {
	var output *byte
	code := bindings.embeddingCatalog(&output)
	if output != nil {
		defer bindings.freeBuffer(output)
	}
	if code < 0 {
		return EmbeddingCatalogData{}, fmt.Errorf(
			"moonshine: get embedding catalog: %w",
			nativeError(code, bindings.errorToString(code)),
		)
	}
	if output == nil {
		return EmbeddingCatalogData{}, fmt.Errorf("moonshine: get embedding catalog returned nil: %w", ErrInvalidNativeOutput)
	}
	var catalog EmbeddingCatalogData
	if err := json.Unmarshal([]byte(copyCString(output)), &catalog); err != nil {
		return EmbeddingCatalogData{}, fmt.Errorf("moonshine: decode embedding catalog: %w: %w", ErrInvalidNativeOutput, err)
	}
	return catalog, nil
}
