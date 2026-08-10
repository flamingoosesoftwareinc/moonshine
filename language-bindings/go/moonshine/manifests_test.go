package moonshine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeManifestBindings struct {
	sttCode              int32
	g2pCode              int32
	ttsCode              int32
	diarCode             int32
	catalogCode          int32
	embeddingCode        int32
	embeddingCatalogCode int32
	errorMessage         string
	output               []byte
	languages            []string
	options              [][]Option
	freed                []*byte
}

func (f *fakeManifestBindings) g2pDependencies(
	languages string, options []Option, output **byte,
) int32 {
	f.languages = append(f.languages, languages)
	f.options = append(f.options, append([]Option(nil), options...))
	f.setOutput(output)
	return f.g2pCode
}

func (f *fakeManifestBindings) ttsDependencies(
	languages string, options []Option, output **byte,
) int32 {
	f.languages = append(f.languages, languages)
	f.options = append(f.options, append([]Option(nil), options...))
	f.setOutput(output)
	return f.ttsCode
}

func (f *fakeManifestBindings) setOutput(output **byte) {
	if len(f.output) > 0 {
		*output = &f.output[0]
	}
}

func (f *fakeManifestBindings) sttDependencies(language string, options []Option, output **byte) int32 {
	f.languages = append(f.languages, language)
	f.options = append(f.options, append([]Option(nil), options...))
	f.setOutput(output)
	return f.sttCode
}

func (f *fakeManifestBindings) diarizationDependencies(output **byte) int32 {
	f.setOutput(output)
	return f.diarCode
}

func (f *fakeManifestBindings) sttCatalog(output **byte) int32 {
	f.setOutput(output)
	return f.catalogCode
}

func (f *fakeManifestBindings) embeddingDependencies(model string, options []Option, output **byte) int32 {
	f.languages = append(f.languages, model)
	f.options = append(f.options, append([]Option(nil), options...))
	f.setOutput(output)
	return f.embeddingCode
}

func (f *fakeManifestBindings) embeddingCatalog(output **byte) int32 {
	f.setOutput(output)
	return f.embeddingCatalogCode
}

func (f *fakeManifestBindings) freeBuffer(pointer *byte) {
	f.freed = append(f.freed, pointer)
}

func (f *fakeManifestBindings) errorToString(int32) string { return f.errorMessage }

func manifestJSON(value string) []byte { return append([]byte(value), 0) }

func TestG2PDependenciesSplitsKeysAndFreesNativeOutput(t *testing.T) {
	bindings := &fakeManifestBindings{output: manifestJSON("en_us/g2p-config.json,en_us/oov/model.ort")}
	options := []Option{{Name: "g2p_root", Value: "/models/tts"}}

	keys, err := g2pDependencies(bindings, []string{"en_us", "es_mx"}, options...)

	require.NoError(t, err)
	assert.Equal(t, []string{"en_us/g2p-config.json", "en_us/oov/model.ort"}, keys)
	assert.Equal(t, []string{"en_us,es_mx"}, bindings.languages)
	assert.Equal(t, [][]Option{options}, bindings.options)
	assert.Equal(t, []*byte{&bindings.output[0]}, bindings.freed)
}

func TestG2PDependenciesAllowsAllLanguagesAndEmptyResult(t *testing.T) {
	bindings := &fakeManifestBindings{output: manifestJSON("")}

	keys, err := g2pDependencies(bindings, nil)

	require.NoError(t, err)
	assert.Empty(t, keys)
	assert.Equal(t, []string{""}, bindings.languages)
	assert.Equal(t, []*byte{&bindings.output[0]}, bindings.freed)
}

func TestG2PDependenciesMapsNativeErrorAndFreesUnexpectedOutput(t *testing.T) {
	bindings := &fakeManifestBindings{
		g2pCode: rawErrorInvalidArgument, errorMessage: "Invalid argument",
		output: manifestJSON("unexpected"),
	}

	_, err := g2pDependencies(bindings, []string{"unknown"})

	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Equal(t, []*byte{&bindings.output[0]}, bindings.freed)
}

func TestG2PDependenciesRejectsInvalidInputAndOutput(t *testing.T) {
	for _, languages := range [][]string{{""}, {"en\x00us"}, {"en_us,es_mx"}} {
		bindings := &fakeManifestBindings{}
		_, err := g2pDependencies(bindings, languages)
		require.ErrorIs(t, err, ErrInvalidArgument)
		assert.Empty(t, bindings.languages)
	}
	bindings := &fakeManifestBindings{}
	_, err := g2pDependencies(bindings, []string{"en_us"}, Option{Name: ""})
	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Empty(t, bindings.languages)

	bindings = &fakeManifestBindings{}
	_, err = g2pDependencies(bindings, []string{"en_us"})
	require.ErrorIs(t, err, ErrInvalidNativeOutput)

	bindings = &fakeManifestBindings{output: manifestJSON("one,,two")}
	_, err = g2pDependencies(bindings, []string{"en_us"})
	require.ErrorIs(t, err, ErrInvalidNativeOutput)
	assert.Equal(t, []*byte{&bindings.output[0]}, bindings.freed)
}

func TestTTSDependenciesDecodesForwardsAndFreesManifest(t *testing.T) {
	bindings := &fakeManifestBindings{output: manifestJSON(`{
		"groups":[{"base_url":"https://example.test/tts","files":[
			{"name":"en_us/model.ort","url":"https://example.test/tts/en_us/model.ort","size":42,"checksum":"abc","checksum_type":"crc32c"}
		]}]}`)}
	options := []Option{{Name: "voice", Value: "piper_en_US-amy-low"}}

	manifest, err := ttsDependencies(bindings, []string{"en_us", "es_mx"}, options...)

	require.NoError(t, err)
	require.Len(t, manifest.Groups, 1)
	assert.Equal(t, "en_us/model.ort", manifest.Groups[0].Files[0].Name)
	assert.Equal(t, []string{"en_us,es_mx"}, bindings.languages)
	assert.Equal(t, [][]Option{options}, bindings.options)
	assert.Equal(t, []*byte{&bindings.output[0]}, bindings.freed)
}

func TestTTSDependenciesAllowsAllLanguages(t *testing.T) {
	bindings := &fakeManifestBindings{output: manifestJSON(`{"groups":[]}`)}
	manifest, err := ttsDependencies(bindings, nil)
	require.NoError(t, err)
	assert.Empty(t, manifest.Groups)
	assert.Equal(t, []string{""}, bindings.languages)
}

func TestTTSDependenciesMapsErrorsAndRejectsInvalidInput(t *testing.T) {
	bindings := &fakeManifestBindings{
		ttsCode: rawErrorInvalidArgument, errorMessage: "Invalid argument",
		output: manifestJSON(`{"groups":[]}`),
	}
	_, err := ttsDependencies(bindings, []string{"unknown"})
	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Equal(t, []*byte{&bindings.output[0]}, bindings.freed)

	for _, languages := range [][]string{{""}, {"en\x00us"}, {"en_us,es_mx"}} {
		bindings = &fakeManifestBindings{}
		_, err = ttsDependencies(bindings, languages)
		require.ErrorIs(t, err, ErrInvalidArgument)
		assert.Empty(t, bindings.languages)
	}
	bindings = &fakeManifestBindings{}
	_, err = ttsDependencies(bindings, []string{"en_us"}, Option{})
	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Empty(t, bindings.languages)
}

func TestSTTDependenciesDecodesAndFreesNativeManifest(t *testing.T) {
	bindings := &fakeManifestBindings{output: manifestJSON(`{
		"groups":[{"base_url":"https://example.test/model","files":[
			{"name":"model.ort","url":"https://example.test/model/model.ort","size":42,"checksum":"abc","checksum_type":"crc32c"},
			{"name":"tokenizer.bin","url":"https://example.test/model/tokenizer.bin","size":null,"checksum":"","checksum_type":""}
		]}]}`)}
	options := []Option{{Name: "model_arch", Value: "0"}}

	manifest, err := sttDependencies(bindings, "en", options...)

	require.NoError(t, err)
	require.Len(t, manifest.Groups, 1)
	assert.Equal(t, "https://example.test/model", manifest.Groups[0].BaseURL)
	require.Len(t, manifest.Groups[0].Files, 2)
	assert.Equal(t, "model.ort", manifest.Groups[0].Files[0].Name)
	require.NotNil(t, manifest.Groups[0].Files[0].Size)
	assert.Equal(t, uint64(42), *manifest.Groups[0].Files[0].Size)
	assert.Nil(t, manifest.Groups[0].Files[1].Size)
	assert.Equal(t, []string{"en"}, bindings.languages)
	assert.Equal(t, [][]Option{options}, bindings.options)
	assert.Equal(t, []*byte{&bindings.output[0]}, bindings.freed)
}

func TestDiarizationDependenciesDecodesAndFreesNativeManifest(t *testing.T) {
	bindings := &fakeManifestBindings{output: manifestJSON(`{"groups":[]}`)}

	manifest, err := diarizationDependencies(bindings)

	require.NoError(t, err)
	assert.Empty(t, manifest.Groups)
	assert.Equal(t, []*byte{&bindings.output[0]}, bindings.freed)
}

func TestManifestNativeErrorUsesSentinelAndFreesUnexpectedOutput(t *testing.T) {
	bindings := &fakeManifestBindings{
		sttCode:      rawErrorInvalidArgument,
		errorMessage: "Invalid argument",
		output:       manifestJSON(`{"groups":[]}`),
	}

	_, err := sttDependencies(bindings, "unknown")

	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Equal(t, []*byte{&bindings.output[0]}, bindings.freed)
}

func TestManifestRejectsNilAndMalformedNativeOutput(t *testing.T) {
	tests := []struct {
		name   string
		output []byte
	}{
		{name: "nil"},
		{name: "malformed", output: manifestJSON(`{`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bindings := &fakeManifestBindings{output: test.output}

			_, err := diarizationDependencies(bindings)

			require.ErrorIs(t, err, ErrInvalidNativeOutput)
			if len(test.output) == 0 {
				assert.Empty(t, bindings.freed)
			} else {
				assert.Equal(t, []*byte{&bindings.output[0]}, bindings.freed)
			}
		})
	}
}

func TestSTTDependenciesRejectsInvalidInputBeforeNativeCall(t *testing.T) {
	tests := []struct {
		name     string
		language string
		options  []Option
	}{
		{name: "empty language"},
		{name: "NUL language", language: "e\x00n"},
		{name: "invalid option", language: "en", options: []Option{{Name: ""}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bindings := &fakeManifestBindings{}

			_, err := sttDependencies(bindings, test.language, test.options...)

			require.ErrorIs(t, err, ErrInvalidArgument)
			assert.Empty(t, bindings.languages)
		})
	}
}
