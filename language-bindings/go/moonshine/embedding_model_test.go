package moonshine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEmbeddingBindings struct {
	handle          int32
	errorMessage    string
	paths           []string
	arches          []uint32
	variants        []string
	filenames       [][]string
	memory          [][][]byte
	sizes           [][]uint64
	options         [][]Option
	freed           []int32
	embedding       []float32
	embeddingSize   uint64
	embeddingCode   int32
	embeddingTexts  []string
	embeddingNames  []string
	freedEmbeddings []*float32
	similarity      float32
	similarityCode  int32
	similarityA     [][]float32
	similarityB     [][]float32
}

func (f *fakeEmbeddingBindings) calculateEmbedding(
	_ int32,
	text, modelName string,
) (*float32, uint64, int32) {
	f.embeddingTexts = append(f.embeddingTexts, text)
	f.embeddingNames = append(f.embeddingNames, modelName)
	var pointer *float32
	if len(f.embedding) > 0 {
		pointer = &f.embedding[0]
	}
	size := f.embeddingSize
	if size == 0 && pointer != nil {
		size = uint64(len(f.embedding))
	}
	return pointer, size, f.embeddingCode
}

func (f *fakeEmbeddingBindings) freeEmbedding(pointer *float32) {
	f.freedEmbeddings = append(f.freedEmbeddings, pointer)
}

func (f *fakeEmbeddingBindings) calculateEmbeddingDistance(
	_ int32,
	a, b []float32,
) (float32, int32) {
	f.similarityA = append(f.similarityA, append([]float32(nil), a...))
	f.similarityB = append(f.similarityB, append([]float32(nil), b...))
	return f.similarity, f.similarityCode
}

func (f *fakeEmbeddingBindings) createEmbeddingModel(path string, arch uint32, variant string) int32 {
	f.paths = append(f.paths, path)
	f.arches = append(f.arches, arch)
	f.variants = append(f.variants, variant)
	return f.handle
}

func (f *fakeEmbeddingBindings) createEmbeddingModelFromMemory(
	arch uint32,
	variant string,
	filenames []string,
	memory [][]byte,
	sizes []uint64,
	options []Option,
) int32 {
	f.arches = append(f.arches, arch)
	f.variants = append(f.variants, variant)
	f.filenames = append(f.filenames, append([]string(nil), filenames...))
	f.memory = append(f.memory, append([][]byte(nil), memory...))
	f.sizes = append(f.sizes, append([]uint64(nil), sizes...))
	f.options = append(f.options, append([]Option(nil), options...))
	return f.handle
}

func (f *fakeEmbeddingBindings) freeEmbeddingModel(handle int32) { f.freed = append(f.freed, handle) }
func (f *fakeEmbeddingBindings) errorToString(int32) string      { return f.errorMessage }

func TestNewEmbeddingModelCreatesAndClosesNativeHandle(t *testing.T) {
	bindings := &fakeEmbeddingBindings{handle: 42}

	model, err := newEmbeddingModel(bindings, "/models/embedding", EmbeddingModelArchGemma300M, "q4")

	require.NoError(t, err)
	assert.Equal(t, []string{"/models/embedding"}, bindings.paths)
	assert.Equal(t, []uint32{uint32(EmbeddingModelArchGemma300M)}, bindings.arches)
	assert.Equal(t, []string{"q4"}, bindings.variants)
	require.NoError(t, model.Close())
	require.NoError(t, model.Close())
	assert.Equal(t, []int32{42}, bindings.freed)
}

func TestNewEmbeddingModelFromMemorySortsAndForwardsFiles(t *testing.T) {
	bindings := &fakeEmbeddingBindings{handle: 42}
	files := map[string][]byte{"tokenizer.bin": {3}, "model_q4.ort": {1, 2}}
	options := []Option{{Name: "ort_providers", Value: "CPU"}}

	model, err := newEmbeddingModelFromMemory(
		bindings, files, EmbeddingModelArchGemma300M, "q4", options...,
	)

	require.NoError(t, err)
	assert.Equal(t, [][]string{{"model_q4.ort", "tokenizer.bin"}}, bindings.filenames)
	assert.Equal(t, [][][]byte{{{1, 2}, {3}}}, bindings.memory)
	assert.Equal(t, [][]uint64{{2, 1}}, bindings.sizes)
	assert.Equal(t, [][]Option{options}, bindings.options)
	require.NoError(t, model.Close())
}

func TestEmbeddingConstructorsMapNativeErrors(t *testing.T) {
	bindings := &fakeEmbeddingBindings{
		handle:       rawErrorInvalidArgument,
		errorMessage: "Invalid argument",
	}

	model, err := newEmbeddingModel(bindings, "/missing", EmbeddingModelArchGemma300M, "q4")

	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Nil(t, model)
	assert.Empty(t, bindings.freed)
}

func TestEmbeddingConstructorsRejectInvalidInputBeforeNativeCall(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		variant string
		files   map[string][]byte
		memory  bool
		options []Option
	}{
		{name: "empty path"},
		{name: "NUL path", path: "bad\x00path"},
		{name: "NUL file variant", path: "/models", variant: "q\x004"},
		{name: "empty memory map", memory: true},
		{name: "empty memory filename", memory: true, files: map[string][]byte{"": {1}}},
		{name: "NUL memory filename", memory: true, files: map[string][]byte{"bad\x00name": {1}}},
		{name: "NUL memory variant", memory: true, files: map[string][]byte{"model.ort": {1}}, variant: "q\x004"},
		{name: "invalid option", memory: true, files: map[string][]byte{"model.ort": {1}}, options: []Option{{Name: ""}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bindings := &fakeEmbeddingBindings{handle: 42}
			var model *EmbeddingModel
			var err error
			if test.memory {
				model, err = newEmbeddingModelFromMemory(
					bindings, test.files, EmbeddingModelArchGemma300M, test.variant, test.options...,
				)
			} else {
				model, err = newEmbeddingModel(
					bindings, test.path, EmbeddingModelArchGemma300M, test.variant,
				)
			}
			require.ErrorIs(t, err, ErrInvalidArgument)
			assert.Nil(t, model)
			assert.Empty(t, bindings.arches)
		})
	}
}

func TestNilEmbeddingModelClose(t *testing.T) {
	var model *EmbeddingModel
	require.NoError(t, model.Close())
}

func TestEmbeddingModelEmbedCopiesAndFreesNativeVector(t *testing.T) {
	bindings := &fakeEmbeddingBindings{handle: 42, embedding: []float32{0.25, -0.5}}
	model, err := newEmbeddingModel(bindings, "/models/embedding", EmbeddingModelArchGemma300M, "q4")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, model.Close()) })

	got, err := model.Embed("hello", "embeddinggemma-300m")

	require.NoError(t, err)
	assert.Equal(t, []float32{0.25, -0.5}, got)
	assert.Equal(t, []string{"hello"}, bindings.embeddingTexts)
	assert.Equal(t, []string{"embeddinggemma-300m"}, bindings.embeddingNames)
	assert.Equal(t, []*float32{&bindings.embedding[0]}, bindings.freedEmbeddings)
	bindings.embedding[0] = 99
	assert.Equal(t, float32(0.25), got[0])
}

func TestEmbeddingModelEmbedFreesUnexpectedOutputOnNativeError(t *testing.T) {
	bindings := &fakeEmbeddingBindings{
		handle:        42,
		embedding:     []float32{1},
		embeddingCode: rawErrorInvalidArgument,
	}
	model, err := newEmbeddingModel(bindings, "/models/embedding", EmbeddingModelArchGemma300M, "q4")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, model.Close()) })

	_, err = model.Embed("hello", "")

	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Equal(t, []*float32{&bindings.embedding[0]}, bindings.freedEmbeddings)
}

func TestEmbeddingModelEmbedRejectsInvalidNativeOutput(t *testing.T) {
	bindings := &fakeEmbeddingBindings{handle: 42, embeddingSize: 2}
	model, err := newEmbeddingModel(bindings, "/models/embedding", EmbeddingModelArchGemma300M, "q4")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, model.Close()) })

	_, err = model.Embed("hello", "")

	require.ErrorIs(t, err, ErrInvalidNativeOutput)
}

func TestEmbeddingModelSimilarityForwardsVectors(t *testing.T) {
	bindings := &fakeEmbeddingBindings{handle: 42, similarity: 0.75}
	model, err := newEmbeddingModel(bindings, "/models/embedding", EmbeddingModelArchGemma300M, "q4")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, model.Close()) })

	got, err := model.Similarity([]float32{1, 2}, []float32{3, 4})

	require.NoError(t, err)
	assert.Equal(t, float32(0.75), got)
	assert.Equal(t, [][]float32{{1, 2}}, bindings.similarityA)
	assert.Equal(t, [][]float32{{3, 4}}, bindings.similarityB)
}

func TestEmbeddingModelSimilarityValidatesDimensions(t *testing.T) {
	bindings := &fakeEmbeddingBindings{handle: 42}
	model, err := newEmbeddingModel(bindings, "/models/embedding", EmbeddingModelArchGemma300M, "q4")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, model.Close()) })

	for _, vectors := range []struct{ a, b []float32 }{
		{},
		{a: []float32{1}, b: []float32{1, 2}},
	} {
		_, err := model.Similarity(vectors.a, vectors.b)
		require.ErrorIs(t, err, ErrInvalidArgument)
	}
	assert.Empty(t, bindings.similarityA)
}

func TestEmbeddingModelOperationsAfterClose(t *testing.T) {
	bindings := &fakeEmbeddingBindings{handle: 42}
	model, err := newEmbeddingModel(bindings, "/models/embedding", EmbeddingModelArchGemma300M, "q4")
	require.NoError(t, err)
	require.NoError(t, model.Close())

	_, err = model.Embed("hello", "")
	require.ErrorIs(t, err, ErrClosed)
	_, err = model.Similarity([]float32{1}, []float32{1})
	require.ErrorIs(t, err, ErrClosed)
}
