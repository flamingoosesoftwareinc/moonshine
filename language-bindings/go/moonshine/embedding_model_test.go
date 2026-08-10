package moonshine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEmbeddingBindings struct {
	handle       int32
	errorMessage string
	paths        []string
	arches       []uint32
	variants     []string
	filenames    [][]string
	memory       [][][]byte
	sizes        [][]uint64
	options      [][]Option
	freed        []int32
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
