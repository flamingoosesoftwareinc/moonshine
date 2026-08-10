package moonshine

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"github.com/moonshine-ai/moonshine/language-bindings/go/raw"
)

// EmbeddingModelArch identifies a native text embedding architecture.
type EmbeddingModelArch uint32

const (
	EmbeddingModelArchGemma300M EmbeddingModelArch = raw.MoonshineEmbeddingModelArchGemma300m
)

type embeddingBindings interface {
	createEmbeddingModel(path string, arch uint32, variant string) int32
	createEmbeddingModelFromMemory(
		arch uint32,
		variant string,
		filenames []string,
		memory [][]byte,
		memorySizes []uint64,
		options []Option,
	) int32
	freeEmbeddingModel(handle int32)
	calculateEmbedding(handle int32, sentence, modelName string) (*float32, uint64, int32)
	freeEmbedding(pointer *float32)
	calculateEmbeddingDistance(handle int32, a, b []float32) (float32, int32)
	errorToString(code int32) string
}

type rawEmbeddingBindings struct{}

func (rawEmbeddingBindings) createEmbeddingModel(path string, arch uint32, variant string) int32 {
	return raw.MoonshineCreateEmbeddingModel(path, arch, variant)
}

func (rawEmbeddingBindings) createEmbeddingModelFromMemory(
	arch uint32,
	variant string,
	filenames []string,
	memory [][]byte,
	memorySizes []uint64,
	options []Option,
) int32 {
	converted := rawOptions(options)
	return raw.MoonshineCreateEmbeddingModelFromMemory(
		arch,
		variant,
		filenames,
		uint64(len(filenames)),
		memory,
		memorySizes,
		converted,
		uint64(len(converted)),
		raw.MoonshineHeaderVersion,
	)
}

func (rawEmbeddingBindings) freeEmbeddingModel(handle int32) {
	raw.MoonshineFreeEmbeddingModel(handle)
}

func (rawEmbeddingBindings) calculateEmbedding(
	handle int32,
	sentence, modelName string,
) (*float32, uint64, int32) {
	output := [][]float32{nil}
	size := []uint64{0}
	code := raw.MoonshineCalculateEmbedding(handle, sentence, output, size, modelName)
	return unsafe.SliceData(output[0]), size[0], code
}

func (rawEmbeddingBindings) freeEmbedding(pointer *float32) {
	if pointer != nil {
		raw.MoonshineFreeEmbedding(unsafe.Slice(pointer, 1))
	}
}

func (rawEmbeddingBindings) calculateEmbeddingDistance(
	handle int32,
	a, b []float32,
) (float32, int32) {
	output := []float32{0}
	code := raw.MoonshineCalculateEmbeddingDistance(handle, a, b, uint64(len(a)), output)
	return output[0], code
}

func (rawEmbeddingBindings) errorToString(code int32) string {
	return copyCString(raw.MoonshineErrorToString(code))
}

// EmbeddingModel owns a native Moonshine text embedding model.
type EmbeddingModel struct {
	bindings  embeddingBindings
	handle    int32
	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once
}

// NewEmbeddingModel loads an embedding model from a directory.
func NewEmbeddingModel(path string, arch EmbeddingModelArch, variant string) (*EmbeddingModel, error) {
	return newEmbeddingModel(rawEmbeddingBindings{}, path, arch, variant)
}

func newEmbeddingModel(
	bindings embeddingBindings,
	path string,
	arch EmbeddingModelArch,
	variant string,
) (*EmbeddingModel, error) {
	if path == "" || strings.IndexByte(path, 0) >= 0 {
		return nil, fmt.Errorf("invalid embedding model path %q: %w", path, ErrInvalidArgument)
	}
	if strings.IndexByte(variant, 0) >= 0 {
		return nil, fmt.Errorf("embedding model variant contains a NUL: %w", ErrInvalidArgument)
	}
	handle := bindings.createEmbeddingModel(path, uint32(arch), variant)
	return newEmbeddingModelOwner(bindings, handle, "create embedding model")
}

// NewEmbeddingModelFromMemory creates an embedding model from canonical asset
// filenames and bytes. The native API copies required bytes during this call,
// so callers may reuse or release their buffers after it returns.
func NewEmbeddingModelFromMemory(
	files map[string][]byte,
	arch EmbeddingModelArch,
	variant string,
	options ...Option,
) (*EmbeddingModel, error) {
	return newEmbeddingModelFromMemory(rawEmbeddingBindings{}, files, arch, variant, options...)
}

func newEmbeddingModelFromMemory(
	bindings embeddingBindings,
	files map[string][]byte,
	arch EmbeddingModelArch,
	variant string,
	options ...Option,
) (*EmbeddingModel, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("embedding model file map is empty: %w", ErrInvalidArgument)
	}
	if strings.IndexByte(variant, 0) >= 0 {
		return nil, fmt.Errorf("embedding model variant contains a NUL: %w", ErrInvalidArgument)
	}
	if err := validateOptions(options); err != nil {
		return nil, err
	}
	filenames := make([]string, 0, len(files))
	for filename := range files {
		if filename == "" || strings.IndexByte(filename, 0) >= 0 {
			return nil, fmt.Errorf("invalid embedding filename %q: %w", filename, ErrInvalidArgument)
		}
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)
	memory := make([][]byte, len(filenames))
	sizes := make([]uint64, len(filenames))
	for index, filename := range filenames {
		memory[index] = files[filename]
		sizes[index] = uint64(len(memory[index]))
	}
	handle := bindings.createEmbeddingModelFromMemory(
		uint32(arch), variant, filenames, memory, sizes, options,
	)
	runtime.KeepAlive(files)
	return newEmbeddingModelOwner(bindings, handle, "create embedding model from memory")
}

func newEmbeddingModelOwner(bindings embeddingBindings, handle int32, operation string) (*EmbeddingModel, error) {
	if handle < 0 {
		return nil, fmt.Errorf(
			"moonshine: %s: %w",
			operation,
			nativeError(handle, bindings.errorToString(handle)),
		)
	}
	model := &EmbeddingModel{bindings: bindings, handle: handle}
	runtime.SetFinalizer(model, (*EmbeddingModel).finalize)
	return model, nil
}

// Close releases the native embedding model. It is idempotent.
func (m *EmbeddingModel) Close() error {
	if m == nil {
		return nil
	}
	m.closeOnce.Do(func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		runtime.SetFinalizer(m, nil)
		m.closed = true
		m.bindings.freeEmbeddingModel(m.handle)
	})
	return nil
}

// Embed calculates a Go-owned embedding vector for text. modelName is reserved
// by the native API for model selection; pass an empty string for the loaded
// model.
func (m *EmbeddingModel) Embed(text, modelName string) ([]float32, error) {
	if m == nil {
		return nil, ErrClosed
	}
	if strings.IndexByte(text, 0) >= 0 || strings.IndexByte(modelName, 0) >= 0 {
		return nil, fmt.Errorf("embedding text or model name contains a NUL: %w", ErrInvalidArgument)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil, ErrClosed
	}
	pointer, size, code := m.bindings.calculateEmbedding(m.handle, text, modelName)
	if pointer != nil {
		defer m.bindings.freeEmbedding(pointer)
	}
	runtime.KeepAlive(m)
	if code < 0 {
		return nil, fmt.Errorf(
			"moonshine: calculate embedding: %w",
			nativeError(code, m.bindings.errorToString(code)),
		)
	}
	if size > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("embedding length %d exceeds addressable memory: %w", size, ErrInvalidNativeOutput)
	}
	if size > 0 && pointer == nil {
		return nil, fmt.Errorf("native embedding returned %d elements with a nil buffer: %w", size, ErrInvalidNativeOutput)
	}
	return append([]float32(nil), unsafe.Slice(pointer, int(size))...), nil
}

// Similarity calculates cosine similarity between equal-length, non-empty
// embedding vectors.
func (m *EmbeddingModel) Similarity(a, b []float32) (float32, error) {
	if m == nil {
		return 0, ErrClosed
	}
	if len(a) == 0 || len(a) != len(b) {
		return 0, fmt.Errorf("embedding dimensions %d and %d are incompatible: %w", len(a), len(b), ErrInvalidArgument)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return 0, ErrClosed
	}
	similarity, code := m.bindings.calculateEmbeddingDistance(m.handle, a, b)
	runtime.KeepAlive(m)
	if code < 0 {
		return 0, fmt.Errorf(
			"moonshine: calculate embedding similarity: %w",
			nativeError(code, m.bindings.errorToString(code)),
		)
	}
	return similarity, nil
}

func (m *EmbeddingModel) finalize() { _ = m.Close() }
