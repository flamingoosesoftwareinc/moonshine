package moonshine

import (
	"encoding/json"
	"fmt"
)

// STTCatalogData contains every speech-to-text language and model published
// by the linked Moonshine library.
type STTCatalogData struct {
	Languages []STTLanguage `json:"languages"`
}

// STTLanguage describes one language in the speech-to-text catalog.
type STTLanguage struct {
	Code        string     `json:"code"`
	EnglishName string     `json:"english_name"`
	Models      []STTModel `json:"models"`
}

// STTModel describes one model architecture available for a language.
type STTModel struct {
	ModelArch   ModelArch `json:"model_arch"`
	DownloadURL string    `json:"download_url"`
	IsDefault   bool      `json:"is_default"`
}

// STTCatalog returns the speech-to-text model catalog from the linked native
// library.
func STTCatalog() (STTCatalogData, error) {
	return sttCatalog(rawManifestBindings{})
}

func sttCatalog(bindings manifestBindings) (STTCatalogData, error) {
	var output *byte
	code := bindings.sttCatalog(&output)
	if output != nil {
		defer bindings.freeBuffer(output)
	}
	if code < 0 {
		return STTCatalogData{}, fmt.Errorf(
			"moonshine: get STT catalog: %w",
			nativeError(code, bindings.errorToString(code)),
		)
	}
	if output == nil {
		return STTCatalogData{}, fmt.Errorf("moonshine: get STT catalog returned nil: %w", ErrInvalidNativeOutput)
	}

	var catalog STTCatalogData
	if err := json.Unmarshal([]byte(copyCString(output)), &catalog); err != nil {
		return STTCatalogData{}, fmt.Errorf("moonshine: decode STT catalog: %w: %w", ErrInvalidNativeOutput, err)
	}
	return catalog, nil
}
