package moonshine

import "github.com/moonshine-ai/moonshine/language-bindings/go/raw"

// HeaderVersion is the version of the C API header used to build this binding.
const HeaderVersion int32 = raw.MoonshineHeaderVersion

type versionBindings interface {
	version() int32
}

type rawVersionBindings struct{}

func (rawVersionBindings) version() int32 {
	return raw.MoonshineGetVersion()
}

// Version returns the loaded native Moonshine library version.
func Version() int32 {
	return version(rawVersionBindings{})
}

func version(bindings versionBindings) int32 {
	return bindings.version()
}
