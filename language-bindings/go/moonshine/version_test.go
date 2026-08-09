package moonshine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeVersionBindings struct {
	value int32
}

func (f fakeVersionBindings) version() int32 {
	return f.value
}

func TestHeaderVersion(t *testing.T) {
	assert.Equal(t, int32(30000), HeaderVersion)
}

func TestVersionUsesNativeBinding(t *testing.T) {
	assert.Equal(t, int32(30207), version(fakeVersionBindings{value: 30207}))
}

func TestNativeVersionIsPositive(t *testing.T) {
	assert.Positive(t, Version())
}
