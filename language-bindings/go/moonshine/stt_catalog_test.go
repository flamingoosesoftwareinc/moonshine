package moonshine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSTTCatalogDecodesAndFreesNativeCatalog(t *testing.T) {
	bindings := &fakeManifestBindings{output: manifestJSON(`{
		"languages":[{"code":"en","english_name":"English","models":[
			{"model_arch":0,"download_url":"https://example.test/tiny-en","is_default":true},
			{"model_arch":1,"download_url":"https://example.test/base-en","is_default":false}
		]}]}`)}

	catalog, err := sttCatalog(bindings)

	require.NoError(t, err)
	require.Len(t, catalog.Languages, 1)
	assert.Equal(t, "en", catalog.Languages[0].Code)
	assert.Equal(t, "English", catalog.Languages[0].EnglishName)
	require.Len(t, catalog.Languages[0].Models, 2)
	assert.Equal(t, ModelArchTiny, catalog.Languages[0].Models[0].ModelArch)
	assert.True(t, catalog.Languages[0].Models[0].IsDefault)
	assert.Equal(t, []*byte{&bindings.output[0]}, bindings.freed)
}

func TestSTTCatalogMapsNativeErrorAndFreesUnexpectedOutput(t *testing.T) {
	bindings := &fakeManifestBindings{
		catalogCode: rawErrorInvalidArgument,
		output:      manifestJSON(`{"languages":[]}`),
	}

	_, err := sttCatalog(bindings)

	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Equal(t, []*byte{&bindings.output[0]}, bindings.freed)
}

func TestSTTCatalogRejectsNilAndMalformedNativeOutput(t *testing.T) {
	for _, test := range []struct {
		name   string
		output []byte
	}{
		{name: "nil"},
		{name: "malformed", output: manifestJSON(`{`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			bindings := &fakeManifestBindings{output: test.output}

			_, err := sttCatalog(bindings)

			require.ErrorIs(t, err, ErrInvalidNativeOutput)
			if len(test.output) == 0 {
				assert.Empty(t, bindings.freed)
			} else {
				assert.Equal(t, []*byte{&bindings.output[0]}, bindings.freed)
			}
		})
	}
}
