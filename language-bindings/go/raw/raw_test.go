package raw_test

import (
	"testing"

	"github.com/moonshine-ai/moonshine/language-bindings/go/raw"
	"github.com/stretchr/testify/assert"
)

func TestMoonshineHeaderVersion(t *testing.T) {
	assert.Equal(t, 30000, raw.MoonshineHeaderVersion)
}
