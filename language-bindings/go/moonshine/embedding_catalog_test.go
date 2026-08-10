package moonshine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddingDependenciesForwardsVariantAndFreesManifest(t *testing.T) {
	bindings := &fakeManifestBindings{output: manifestJSON(`{"groups":[]}`)}

	manifest, err := embeddingDependencies(
		bindings,
		"embeddinggemma-300m",
		"q4",
		Option{Name: "ort_providers", Value: "CPU"},
	)

	require.NoError(t, err)
	assert.Empty(t, manifest.Groups)
	assert.Equal(t, []string{"embeddinggemma-300m"}, bindings.languages)
	assert.Equal(t, [][]Option{{
		{Name: "ort_providers", Value: "CPU"},
		{Name: "variant", Value: "q4"},
	}}, bindings.options)
	assert.Equal(t, []*byte{&bindings.output[0]}, bindings.freed)
}

func TestEmbeddingDependenciesMapsErrorsAndValidatesInput(t *testing.T) {
	bindings := &fakeManifestBindings{embeddingCode: rawErrorInvalidArgument}
	_, err := embeddingDependencies(bindings, "unknown", "q4")
	require.ErrorIs(t, err, ErrInvalidArgument)

	for _, input := range []struct{ model, variant string }{
		{model: "bad\x00model"},
		{variant: "bad\x00variant"},
	} {
		bindings = &fakeManifestBindings{}
		_, err = embeddingDependencies(bindings, input.model, input.variant)
		require.ErrorIs(t, err, ErrInvalidArgument)
		assert.Empty(t, bindings.languages)
	}
}

func TestEmbeddingCatalogDecodesAndFreesNativeCatalog(t *testing.T) {
	bindings := &fakeManifestBindings{output: manifestJSON(`{
		"models":[{"name":"embeddinggemma-300m","english_name":"Embedding Gemma 300M",
		"download_url":"https://example.test/embedding","variants":["q4","fp32"],"default_variant":"q4"}]}`)}

	catalog, err := embeddingCatalog(bindings)

	require.NoError(t, err)
	require.Len(t, catalog.Models, 1)
	assert.Equal(t, "embeddinggemma-300m", catalog.Models[0].Name)
	assert.Equal(t, []string{"q4", "fp32"}, catalog.Models[0].Variants)
	assert.Equal(t, "q4", catalog.Models[0].DefaultVariant)
	assert.Equal(t, []*byte{&bindings.output[0]}, bindings.freed)
}

func TestEmbeddingCatalogRejectsInvalidNativeOutput(t *testing.T) {
	for _, output := range [][]byte{nil, manifestJSON(`{`)} {
		bindings := &fakeManifestBindings{output: output}
		_, err := embeddingCatalog(bindings)
		require.ErrorIs(t, err, ErrInvalidNativeOutput)
	}
}
