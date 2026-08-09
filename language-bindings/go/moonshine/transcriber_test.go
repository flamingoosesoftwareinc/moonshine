package moonshine

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTranscriberBindings struct {
	handle       int32
	errorMessage string
	paths        []string
	arches       []uint32
	options      [][]Option
	filenames    [][]string
	memory       [][][]byte
	memorySizes  [][]uint64
	freed        []int32
	freedHandle  chan int32
}

func (f *fakeTranscriberBindings) loadTranscriberFromMemoryFiles(
	filenames []string,
	memory [][]byte,
	memorySizes []uint64,
	modelArch uint32,
	options []Option,
) int32 {
	f.filenames = append(f.filenames, append([]string(nil), filenames...))
	f.memory = append(f.memory, append([][]byte(nil), memory...))
	f.memorySizes = append(f.memorySizes, append([]uint64(nil), memorySizes...))
	f.arches = append(f.arches, modelArch)
	f.options = append(f.options, append([]Option(nil), options...))
	return f.handle
}

func (f *fakeTranscriberBindings) loadTranscriberFromFiles(path string, modelArch uint32, options []Option) int32 {
	f.paths = append(f.paths, path)
	f.arches = append(f.arches, modelArch)
	f.options = append(f.options, append([]Option(nil), options...))
	return f.handle
}

func (f *fakeTranscriberBindings) freeTranscriber(handle int32) {
	f.freed = append(f.freed, handle)
	if f.freedHandle != nil {
		f.freedHandle <- handle
	}
}

func (f *fakeTranscriberBindings) errorToString(int32) string {
	return f.errorMessage
}

func TestNewTranscriberLoadsAndClosesNativeHandle(t *testing.T) {
	bindings := &fakeTranscriberBindings{handle: 42}

	options := []Option{{Name: "word_timestamps", Value: "true"}}
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny, options...)
	require.NoError(t, err)
	assert.Equal(t, []string{"/models/tiny-en"}, bindings.paths)
	assert.Equal(t, []uint32{uint32(ModelArchTiny)}, bindings.arches)
	assert.Equal(t, [][]Option{options}, bindings.options)

	require.NoError(t, transcriber.Close())
	require.NoError(t, transcriber.Close())

	assert.Equal(t, []int32{42}, bindings.freed)
}

func TestNewTranscriberReturnsLoadError(t *testing.T) {
	bindings := &fakeTranscriberBindings{
		handle:       rawErrorInvalidArgument,
		errorMessage: "Invalid argument",
	}

	transcriber, err := newTranscriber(bindings, "/missing", ModelArchBase)
	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Nil(t, transcriber)
	assert.Empty(t, bindings.freed)
}

func TestNilTranscriberClose(t *testing.T) {
	var transcriber *Transcriber
	require.NoError(t, transcriber.Close())
}

func TestTranscriberCloseIsConcurrentAndIdempotent(t *testing.T) {
	bindings := &fakeTranscriberBindings{handle: 42}
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)

	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_ = transcriber.Close()
		}()
	}
	wait.Wait()

	assert.Equal(t, []int32{42}, bindings.freed)
}

func TestTranscriberFinalizerReleasesAbandonedHandle(t *testing.T) {
	bindings := &fakeTranscriberBindings{
		handle:      42,
		freedHandle: make(chan int32, 1),
	}
	abandonTranscriber(t, bindings)

	deadline := time.After(5 * time.Second)
	for {
		runtime.GC()
		select {
		case handle := <-bindings.freedHandle:
			assert.Equal(t, int32(42), handle)
			return
		case <-deadline:
			t.Fatal("transcriber finalizer did not release the native handle")
		default:
			runtime.Gosched()
		}
	}
}

func abandonTranscriber(t *testing.T, bindings transcriberBindings) {
	t.Helper()
	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	require.NoError(t, err)
	runtime.KeepAlive(transcriber)
}

func TestNewTranscriberFromMemorySortsAndRetainsFilesUntilClose(t *testing.T) {
	bindings := &fakeTranscriberBindings{handle: 42}
	files := map[string][]byte{
		"tokenizer.bin":            {5},
		"encoder_model.ort":        {1, 2},
		"decoder_model_merged.ort": {3, 4, 5},
	}
	options := []Option{{Name: "word_timestamps", Value: "true"}}

	transcriber, err := newTranscriberFromMemory(bindings, files, ModelArchTiny, options...)
	require.NoError(t, err)
	require.NotNil(t, transcriber)

	assert.Equal(t, [][]string{{
		"decoder_model_merged.ort",
		"encoder_model.ort",
		"tokenizer.bin",
	}}, bindings.filenames)
	assert.Equal(t, [][][]byte{{{3, 4, 5}, {1, 2}, {5}}}, bindings.memory)
	assert.Equal(t, [][]uint64{{3, 2, 1}}, bindings.memorySizes)
	assert.Equal(t, []uint32{uint32(ModelArchTiny)}, bindings.arches)
	assert.Equal(t, [][]Option{options}, bindings.options)
	assert.Len(t, transcriber.memory, 3)
	assert.NotNil(t, transcriber.pinner)

	require.NoError(t, transcriber.Close())
	assert.Nil(t, transcriber.memory)
	assert.Nil(t, transcriber.pinner)
	assert.Equal(t, []int32{42}, bindings.freed)
}

func TestNewTranscriberFromMemoryRejectsInvalidFilesBeforeNativeCall(t *testing.T) {
	tests := []struct {
		name  string
		files map[string][]byte
	}{
		{name: "nil"},
		{name: "empty", files: map[string][]byte{}},
		{name: "empty filename", files: map[string][]byte{"": {1}}},
		{name: "NUL filename", files: map[string][]byte{"bad\x00name": {1}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bindings := &fakeTranscriberBindings{handle: 42}

			transcriber, err := newTranscriberFromMemory(bindings, test.files, ModelArchTiny)

			require.ErrorIs(t, err, ErrInvalidArgument)
			assert.Nil(t, transcriber)
			assert.Empty(t, bindings.filenames)
			assert.Empty(t, bindings.freed)
		})
	}
}

func TestNewTranscriberFromMemoryReleasesPinsAfterLoadError(t *testing.T) {
	bindings := &fakeTranscriberBindings{
		handle:       rawErrorInvalidArgument,
		errorMessage: "Invalid argument",
	}

	transcriber, err := newTranscriberFromMemory(
		bindings,
		map[string][]byte{"encoder_model.ort": {1}},
		ModelArchTiny,
	)

	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.Nil(t, transcriber)
	assert.Empty(t, bindings.freed)
}

func TestNewTranscriberRejectsInvalidInputBeforeNativeCall(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		options []Option
	}{
		{name: "NUL path", path: "/models\x00/tiny"},
		{name: "invalid option", path: "/models", options: []Option{{Value: "true"}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bindings := &fakeTranscriberBindings{handle: 42}

			transcriber, err := newTranscriber(bindings, test.path, ModelArchTiny, test.options...)

			require.ErrorIs(t, err, ErrInvalidArgument)
			assert.Nil(t, transcriber)
			assert.Empty(t, bindings.paths)
			assert.Empty(t, bindings.freed)
		})
	}
}

const rawErrorInvalidArgument int32 = -3
