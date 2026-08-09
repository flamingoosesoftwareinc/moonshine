package moonshine

import (
	"slices"
	"strings"
	"testing"
)

type fakeTranscriberBindings struct {
	handle int32
	paths  []string
	arches []uint32
	freed  []int32
}

func (f *fakeTranscriberBindings) loadTranscriberFromFiles(path string, modelArch uint32) int32 {
	f.paths = append(f.paths, path)
	f.arches = append(f.arches, modelArch)
	return f.handle
}

func (f *fakeTranscriberBindings) freeTranscriber(handle int32) {
	f.freed = append(f.freed, handle)
}

func TestNewTranscriberLoadsAndClosesNativeHandle(t *testing.T) {
	bindings := &fakeTranscriberBindings{handle: 42}

	transcriber, err := newTranscriber(bindings, "/models/tiny-en", ModelArchTiny)
	if err != nil {
		t.Fatalf("NewTranscriber() error = %v", err)
	}

	if got, want := bindings.paths, []string{"/models/tiny-en"}; !slices.Equal(got, want) {
		t.Fatalf("load paths = %v, want %v", got, want)
	}
	if got, want := bindings.arches, []uint32{uint32(ModelArchTiny)}; !slices.Equal(got, want) {
		t.Fatalf("model arches = %v, want %v", got, want)
	}

	transcriber.Close()
	transcriber.Close()

	if got, want := bindings.freed, []int32{42}; !slices.Equal(got, want) {
		t.Fatalf("freed handles = %v, want %v", got, want)
	}
}

func TestNewTranscriberReturnsLoadError(t *testing.T) {
	bindings := &fakeTranscriberBindings{handle: rawErrorInvalidArgument}

	transcriber, err := newTranscriber(bindings, "/missing", ModelArchBase)
	if err == nil {
		t.Fatal("NewTranscriber() error = nil, want load error")
	}
	if transcriber != nil {
		t.Fatalf("NewTranscriber() transcriber = %v, want nil", transcriber)
	}
	if !strings.Contains(err.Error(), "error code -3") {
		t.Fatalf("NewTranscriber() error = %q, want error code", err)
	}
	if len(bindings.freed) != 0 {
		t.Fatalf("freed handles = %v, want none", bindings.freed)
	}
}

func TestNilTranscriberClose(t *testing.T) {
	var transcriber *Transcriber
	transcriber.Close()
}

const rawErrorInvalidArgument int32 = -3
