package raw_test

import (
	"testing"

	"github.com/moonshine-ai/moonshine/language-bindings/go/raw"
)

func TestMoonshineHeaderVersion(t *testing.T) {
	if got, want := raw.MoonshineHeaderVersion, 30000; got != want {
		t.Fatalf("HeaderVersion = %d, want %d", got, want)
	}
}
