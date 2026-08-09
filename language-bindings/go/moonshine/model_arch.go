package moonshine

import "github.com/moonshine-ai/moonshine/language-bindings/go/raw"

// ModelArch identifies a Moonshine speech-to-text model architecture.
type ModelArch uint32

const (
	ModelArchTiny            ModelArch = raw.MoonshineModelArchTiny
	ModelArchBase            ModelArch = raw.MoonshineModelArchBase
	ModelArchTinyStreaming   ModelArch = raw.MoonshineModelArchTinyStreaming
	ModelArchBaseStreaming   ModelArch = raw.MoonshineModelArchBaseStreaming
	ModelArchSmallStreaming  ModelArch = raw.MoonshineModelArchSmallStreaming
	ModelArchMediumStreaming ModelArch = raw.MoonshineModelArchMediumStreaming
)
