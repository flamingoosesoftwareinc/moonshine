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

type phonemizerBindings interface {
	createPhonemizerFromFiles(language string, filenames []string, options []Option) int32
	createPhonemizerFromMemory(
		language string,
		filenames []string,
		memory [][]byte,
		memorySizes []uint64,
		options []Option,
	) int32
	freePhonemizer(handle int32)
	textToPhonemes(handle int32, text string, options []Option) (unsafe.Pointer, uint64, int32)
	freeBuffer(pointer unsafe.Pointer)
	errorToString(code int32) string
}

type rawPhonemizerBindings struct{}

func (rawPhonemizerBindings) createPhonemizerFromFiles(
	language string,
	filenames []string,
	options []Option,
) int32 {
	converted := rawOptions(options)
	return raw.MoonshineCreateGraphemeToPhonemizerFromFiles(
		language, filenames, uint64(len(filenames)), converted, uint64(len(converted)),
		raw.MoonshineHeaderVersion,
	)
}

func (rawPhonemizerBindings) createPhonemizerFromMemory(
	language string,
	filenames []string,
	memory [][]byte,
	memorySizes []uint64,
	options []Option,
) int32 {
	converted := rawOptions(options)
	return raw.MoonshineCreateGraphemeToPhonemizerFromMemory(
		language, filenames, uint64(len(filenames)), memory, memorySizes,
		converted, uint64(len(converted)), raw.MoonshineHeaderVersion,
	)
}

func (rawPhonemizerBindings) freePhonemizer(handle int32) {
	raw.MoonshineFreeGraphemeToPhonemizer(handle)
}

func (rawPhonemizerBindings) textToPhonemes(
	handle int32,
	text string,
	options []Option,
) (unsafe.Pointer, uint64, int32) {
	converted := rawOptions(options)
	output := []string{""}
	count := []uint64{0}
	code := raw.MoonshineTextToPhonemes(
		handle, text, converted, uint64(len(converted)), output, count,
	)
	if output[0] == "" {
		return nil, count[0], code
	}
	return unsafe.Pointer(unsafe.StringData(output[0])), count[0], code
}

func (rawPhonemizerBindings) freeBuffer(pointer unsafe.Pointer) {
	raw.MoonshineFreeBuffer(pointer)
}

func (rawPhonemizerBindings) errorToString(code int32) string {
	return copyCString(raw.MoonshineErrorToString(code))
}

// Phonemizer owns a native grapheme-to-phoneme engine. Memory-backed assets
// remain pinned until Close because the native engine retains their pointers.
type Phonemizer struct {
	bindings  phonemizerBindings
	handle    int32
	language  string
	memory    [][]byte
	pinner    *runtime.Pinner
	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once
}

// NewPhonemizerFromFiles creates a phonemizer from canonical asset filenames.
// Assets can also be resolved beneath the g2p_root option.
func NewPhonemizerFromFiles(
	language string,
	files []string,
	options ...Option,
) (*Phonemizer, error) {
	return newPhonemizerFromFiles(rawPhonemizerBindings{}, language, files, options...)
}

func newPhonemizerFromFiles(
	bindings phonemizerBindings,
	language string,
	files []string,
	options ...Option,
) (*Phonemizer, error) {
	if err := validatePhonemizerInput(language, files, options); err != nil {
		return nil, err
	}
	filenames := append([]string(nil), files...)
	handle := bindings.createPhonemizerFromFiles(language, filenames, options)
	return newPhonemizerOwner(bindings, handle, language, nil, nil, "create phonemizer from files")
}

// NewPhonemizerFromMemory creates a phonemizer from canonical asset keys and
// buffers. Non-empty buffers are pinned and must not be modified until Close.
func NewPhonemizerFromMemory(
	language string,
	files map[string][]byte,
	options ...Option,
) (*Phonemizer, error) {
	return newPhonemizerFromMemory(rawPhonemizerBindings{}, language, files, options...)
}

func newPhonemizerFromMemory(
	bindings phonemizerBindings,
	language string,
	files map[string][]byte,
	options ...Option,
) (*Phonemizer, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("phonemizer file map is empty: %w", ErrInvalidArgument)
	}
	filenames := make([]string, 0, len(files))
	for filename := range files {
		filenames = append(filenames, filename)
	}
	if err := validatePhonemizerInput(language, filenames, options); err != nil {
		return nil, err
	}
	sort.Strings(filenames)
	memory := make([][]byte, len(filenames))
	sizes := make([]uint64, len(filenames))
	pinner := new(runtime.Pinner)
	for index, filename := range filenames {
		memory[index] = files[filename]
		sizes[index] = uint64(len(memory[index]))
		if len(memory[index]) > 0 {
			pinner.Pin(&memory[index][0])
		}
	}
	handle := bindings.createPhonemizerFromMemory(language, filenames, memory, sizes, options)
	if handle < 0 {
		pinner.Unpin()
		return nil, fmt.Errorf(
			"moonshine: create phonemizer from memory: %w",
			nativeError(handle, bindings.errorToString(handle)),
		)
	}
	return newPhonemizerOwner(bindings, handle, language, memory, pinner, "")
}

func validatePhonemizerInput(language string, filenames []string, options []Option) error {
	if language == "" || strings.IndexByte(language, 0) >= 0 {
		return fmt.Errorf("invalid phonemizer language %q: %w", language, ErrInvalidArgument)
	}
	if err := validateOptions(options); err != nil {
		return err
	}
	for _, filename := range filenames {
		if filename == "" || strings.IndexByte(filename, 0) >= 0 {
			return fmt.Errorf("invalid phonemizer filename %q: %w", filename, ErrInvalidArgument)
		}
	}
	return nil
}

func newPhonemizerOwner(
	bindings phonemizerBindings,
	handle int32,
	language string,
	memory [][]byte,
	pinner *runtime.Pinner,
	operation string,
) (*Phonemizer, error) {
	if handle < 0 {
		return nil, fmt.Errorf(
			"moonshine: %s: %w",
			operation,
			nativeError(handle, bindings.errorToString(handle)),
		)
	}
	phonemizer := &Phonemizer{
		bindings: bindings, handle: handle, language: language, memory: memory, pinner: pinner,
	}
	runtime.SetFinalizer(phonemizer, (*Phonemizer).finalize)
	return phonemizer, nil
}

// Language returns the language tag used to construct the phonemizer.
func (p *Phonemizer) Language() string {
	if p == nil {
		return ""
	}
	return p.language
}

// Phonemes converts text to a Go-owned International Phonetic Alphabet (IPA)
// string. The result remains valid after later calls and after Close.
func (p *Phonemizer) Phonemes(text string, options ...Option) (string, error) {
	if p == nil {
		return "", ErrClosed
	}
	if strings.IndexByte(text, 0) >= 0 {
		return "", fmt.Errorf("phonemizer text contains a NUL: %w", ErrInvalidArgument)
	}
	if err := validateOptions(options); err != nil {
		return "", err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return "", ErrClosed
	}
	pointer, count, code := p.bindings.textToPhonemes(p.handle, text, options)
	if pointer != nil {
		defer p.bindings.freeBuffer(pointer)
	}
	runtime.KeepAlive(p)
	if code < 0 {
		return "", fmt.Errorf(
			"moonshine: convert text to phonemes: %w",
			nativeError(code, p.bindings.errorToString(code)),
		)
	}
	if count > uint64(^uint(0)>>1) {
		return "", fmt.Errorf("phoneme length %d exceeds addressable memory: %w", count, ErrInvalidNativeOutput)
	}
	if count > 0 && pointer == nil {
		return "", fmt.Errorf("native phonemizer returned %d bytes with a nil buffer: %w", count, ErrInvalidNativeOutput)
	}
	if count == 0 {
		return "", nil
	}
	return strings.Clone(unsafe.String((*byte)(pointer), int(count))), nil
}

// Close releases the native phonemizer and any pinned model buffers.
func (p *Phonemizer) Close() error {
	if p == nil {
		return nil
	}
	p.closeOnce.Do(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		runtime.SetFinalizer(p, nil)
		p.closed = true
		p.bindings.freePhonemizer(p.handle)
		if p.pinner != nil {
			p.pinner.Unpin()
			p.pinner = nil
		}
		p.memory = nil
	})
	return nil
}

func (p *Phonemizer) finalize() { _ = p.Close() }
