# Go language binding plan

This document is the implementation and coverage roadmap for the Moonshine Go
binding. The C API in `core/moonshine-c-api.h` is the source of truth. The
Python, Swift, and Android bindings define the expected public behavior and
integration-test coverage.

## Goals

- Keep `raw` generated and mechanically close to the C ABI.
- Put ownership, cleanup, validation, and Go-native types in `moonshine`.
- Never expose a C-allocated pointer from the public package.
- Make every owning public type implement an idempotent `Close()` method.
- Use finalizers only as a fallback; examples and tests must close explicitly.
- Keep native calls injectable so ownership and error paths have fast unit
  tests that do not load models.
- Add real native integration tests that mirror the other language bindings.
- Implement one public API increment per commit, verify it with
  `scripts/build-go.sh`, then push it.

## Architectural invariant

The dependency direction is always:

```text
application -> moonshine (safe public API) -> internal binding interfaces -> raw (generated cgo) -> C API
```

The public package must not call generated package functions throughout its
domain logic. Each native capability sits behind the smallest useful internal
interface. Production adapters delegate to `raw`; unit tests inject fakes. This
keeps lifecycle, conversion, error, and orchestration tests deterministic and
model-free while native integration tests separately prove the adapter and ABI.

Parity means behavioral parity, not a line-for-line port. Swift and Python are
the primary reference bindings. Android is an additional reference where it
has stronger streaming, cadence, or lifecycle coverage.

## Testing rules

Every increment must include the tests appropriate to that increment:

1. Unit tests use Testify and injected native bindings.
2. Ownership tests verify each successful allocation is freed exactly once.
3. Failure tests verify partial construction does not leak or double-free.
4. Conversion tests cover empty, nil, Unicode, and multiple-element inputs.
5. Native integration tests use `test-assets/tiny-en`, `beckett.wav`, and
   `two_cities.wav` where the equivalent Swift or Android test does.
6. Integration assertions test behavior, not just a non-nil result. For
   transcription this includes expected phrases and line metadata.
7. Tests which need large optional assets must be explicitly gated and explain
   how to enable them. Core Tiny English parity tests run by default.
8. `scripts/build-go.sh` is the required verification entry point because it
   builds and locates `libmoonshine` and ONNX Runtime before running Go tests.

## Package organization

```text
language-bindings/go/
  raw/                    generated C ABI bindings; no hand edits
  moonshine/              safe public Go package
    errors.go
    options.go
    model_arch.go
    transcript.go
    transcriber.go
    stream.go
    embedding.go
    catalog.go
    tts.go
    speech_clip.go
    g2p.go
    wav.go
  internal/testasset/     shared integration-test asset/WAV helpers
```

Files may be split further when a module becomes deep enough to warrant it.

## Public API conventions

- Constructors are named `NewTypeFromFiles` and `NewTypeFromMemory` when both
  forms exist. `NewTranscriber` remains the concise files-based default.
- File maps use `map[string][]byte` at the public boundary.
- Advanced C options use `Option{Name, Value string}`. Convenience options may
  be added later without removing the generic escape hatch.
- Audio is mono float PCM represented as `[]float32`; sample rates are `int` at
  the public boundary and checked before conversion to `int32`.
- Native handles remain private.
- Public result structs contain only Go-owned strings and slices.
- Methods return typed errors wrapping a stable native error code.
- Methods on a closed owner return `ErrClosed` and never call C.
- Concurrent-use guarantees must be documented per owner. `Close` must be safe
  to call concurrently and more than once.

## Incremental implementation sequence

Each numbered item is intended to be one focused commit unless its test fixture
must be committed immediately beforehand.

### 1. Raw ABI verification

- Add compile-time coverage for every C constant, struct, and function.
- Audit generated pointer classifications, especially `T **`, `char **`, and
  library-owned transcript pointers.
- Add manifest pointer hints or a minimal C shim where `c-for-go` cannot express
  the ABI correctly.
- Add a deterministic-generation check: regenerate and require a clean diff.
- Test constant values and C/Go struct sizes and field offsets where cgo makes
  those observable.

### 2. Errors

Public API:

```go
type ErrorCode int32
type Error struct { Code ErrorCode; Message string }
var ErrClosed error
func (e *Error) Error() string
func (e *Error) Is(target error) bool
```

Native symbols: `moonshine_error_to_string`.

Tests:

- Known codes map to unknown, invalid-handle, and invalid-argument errors.
- Unknown negative codes remain available through `ErrorCode`.
- Native error strings are copied into Go memory.
- Nil native error strings have a deterministic fallback message.
- `errors.Is` works for stable categories.

### 3. Options

Public API:

```go
type Option struct { Name, Value string }
```

Tests:

- Empty option lists become nil pointer plus zero count.
- Multiple options preserve order, names, values, and UTF-8.
- C strings remain alive for the duration of a native call.
- Empty names and embedded NUL bytes are rejected before calling C.

### 4. Transcriber construction and lifecycle

Public API:

```go
func NewTranscriber(path string, arch ModelArch, options ...Option) (*Transcriber, error)
func NewTranscriberFromMemory(files map[string][]byte, arch ModelArch, options ...Option) (*Transcriber, error)
func (t *Transcriber) Close() error
```

Native symbols:

- `moonshine_load_transcriber_from_files`
- `moonshine_load_transcriber_from_memory_files`
- deprecated `moonshine_load_transcriber_from_memory` remains available only in
  `raw`; do not promote it in the public package.
- `moonshine_free_transcriber`

Tests:

- Unit: arguments, header version, negative-handle errors, idempotent close,
  concurrent close, nil close, finalizer fallback, and no free after failed
  construction.
- Unit: memory file names, buffers, sizes, and lifetimes are passed correctly.
- Native: load Tiny English from files and close it.
- Native: load Tiny English from a memory-file map and close it.
- Native: invalid/missing model paths return a typed error.

Status: basic files-based construction and idempotent close exist. Options,
typed errors, memory construction, and native parity tests remain.

### 5. Version

Public API:

```go
func Version() int32
const HeaderVersion = ...
```

Native symbol: `moonshine_get_version`.

Tests:

- Generated header version equals the public constant.
- Loaded native library version is positive.
- Mirror Swift `testGetVersion` without requiring a transcriber instance.

### 6. Transcript value types and conversion

Public API:

```go
type WordTiming struct { Text string; Start, End, Confidence float32 }
type SpeakerSpan struct { ... }
type TranscriptLine struct { ... }
type Transcript struct { Lines []TranscriptLine }
func (t Transcript) String() string
```

Native types:

- `transcript_word_t`
- `speaker_span_t`
- `transcript_line_t`
- `transcript_t`

Native symbol: `moonshine_transcript_to_string` is diagnostic only; public
conversion must copy all native data before the next transcriber call.

Tests:

- Empty transcript conversion.
- Multiple lines, words, speaker spans, UTF-8 text, audio, IDs, timestamps,
  confidence, latency, and all change flags.
- Converted values remain valid after the backing native fixture changes.
- Nil pointers with zero counts become empty Go slices safely.
- Inconsistent pointer/count pairs fail safely rather than dereferencing nil.
- String output contains useful line text.

### 7. Non-streaming transcription

Public API:

```go
type TranscribeFlags uint32
const (FlagForceUpdate ...; FlagSpellingMode ...)
func (t *Transcriber) Transcribe(audio []float32, sampleRate int, flags ...TranscribeFlags) (Transcript, error)
```

Native symbol: `moonshine_transcribe_without_streaming`.

Tests:

- Unit: closed transcriber, empty audio, sample-rate bounds, flags, native
  errors, and transcript copying.
- Native: transcribe `beckett.wav`; require lines, text, positive timing.
- Native: transcribe `two_cities.wav`; require “best of times” and “worst of
  times”, complete/new/updated/text-changed flags, and non-empty lines.
- Native: empty audio returns an empty transcript.
- Native: spelling flag with no spelling model is safe and returns empty output
  for empty audio.

### 8. WAV test utility

Internal/public decision: begin under `internal/testasset`; promote to public
`LoadWAV` only if parity with Swift/Android is useful to Go callers.

Tests:

- Decode checked-in PCM WAV fixtures.
- Reject unsupported encoding, malformed chunks, truncated samples, and
  multi-channel input unless conversion is explicitly implemented.

### 9. Stream lifecycle

Public API:

```go
func (t *Transcriber) NewStream(flags ...TranscribeFlags) (*Stream, error)
func (s *Stream) Start() error
func (s *Stream) Stop() error
func (s *Stream) Close() error
```

Native symbols:

- `moonshine_create_stream`
- `moonshine_start_stream`
- `moonshine_stop_stream`
- `moonshine_free_stream`

Tests:

- Unit: handle creation errors; parent/child ownership; start/stop ordering;
  idempotent close; close before start; close after stop; parent close closes
  streams first; operations after either owner closes return `ErrClosed`.
- Native: create, start, stop, and free a stream against Tiny English.
- Native: spelling-mode stream creation is accepted.

### 10. Streaming audio and transcript updates

Public API:

```go
func (s *Stream) AddAudio(audio []float32, sampleRate int, flags ...TranscribeFlags) error
func (s *Stream) Transcript(flags ...TranscribeFlags) (Transcript, error)
```

Native symbols:

- `moonshine_transcribe_add_audio_to_stream`
- `moonshine_transcribe_stream`

Tests:

- Unit: audio/count/rate/flag forwarding, empty audio, native errors, and
  closed-state handling.
- Native: stream `beckett.wav` and `two_cities.wav` in 100 ms chunks.
- Native: final transcript is non-empty and contains expected phrases.
- Native: empty streaming audio returns an empty transcript.
- Native: repeated manual updates return valid snapshots.
- Verify line IDs are stable and only the final line may be incomplete.

### 11. Streaming events and update cadence

Public API should derive events in Go from successive transcript snapshots:
line started, updated, text changed, speakers changed, and completed. Do not add
a C callback layer because the C API exposes snapshot flags already.

Tests mirror Python/Swift/Android:

- Started, updated, text-changed, speakers-changed, and completed events.
- Started count matches completed count after a flushed stream.
- Update cadence is measured in audio duration, not number of chunks.
- A pass waits for at least as much audio as the prior pass cost.
- A pathological slow pass has a bounded delay.
- Cadence is tracked independently per stream.
- Zero interval transcribes every call.
- Stop always flushes regardless of cadence.
- Streaming latency test for `two_cities.wav` uses the same threshold and
  measurement definition as existing bindings.

### 12. STT and diarization manifests/catalog

Public API:

```go
func STTDependencies(language string, options ...Option) (DownloadManifest, error)
func DiarizationDependencies() (DownloadManifest, error)
func STTCatalog() (STTCatalogData, error)
```

Native symbols:

- `moonshine_get_stt_dependencies`
- `moonshine_get_diarization_dependencies`
- `moonshine_get_stt_catalog`
- `moonshine_free_buffer`

Tests:

- Unit: returned C strings are copied and freed exactly once on success and
  malformed JSON; nil output and native errors do not leak.
- Native: known English manifest has groups and required Tiny model files.
- Native: catalog contains English and at least one model.
- Native: diarization manifest contains segmentation and embedding assets.
- JSON fixtures cover unknown fields for forward compatibility.

### 13. Asset downloader and model cache

This is not in the C API but exists in Swift, Android, and Python. Implement as
a separate Go module boundary after manifest APIs stabilize.

Public responsibilities:

- Download grouped manifest files with resumable/atomic writes.
- Validate size and checksum.
- Preserve canonical relative paths and prevent path traversal.
- Allow caller-provided `http.Client`, destination, progress callback, and
  cancellation context.

Tests:

- `httptest.Server` success, resume, checksum mismatch, interrupted download,
  atomic rename, cancellation, duplicate files, and traversal rejection.
- Optional CDN integration test gated by an environment variable.

### 14. Embedding model lifecycle

Public API:

```go
type EmbeddingModelArch uint32
func NewEmbeddingModel(path string, arch EmbeddingModelArch, variant string) (*EmbeddingModel, error)
func NewEmbeddingModelFromMemory(files map[string][]byte, arch EmbeddingModelArch, variant string, options ...Option) (*EmbeddingModel, error)
func (m *EmbeddingModel) Close() error
```

Native symbols:

- `moonshine_create_embedding_model`
- `moonshine_create_embedding_model_from_memory`
- `moonshine_free_embedding_model`

Tests:

- Same constructor/ownership matrix as `Transcriber`.
- Native construction from files and memory using the checked-in/downloaded
  embedding fixture when available.

### 15. Embedding calculation and distance

Public API:

```go
func (m *EmbeddingModel) Embed(text string, modelName string) ([]float32, error)
func (m *EmbeddingModel) Similarity(a, b []float32) (float32, error)
```

Native symbols:

- `moonshine_calculate_embedding`
- `moonshine_free_embedding`
- `moonshine_calculate_embedding_distance`

Tests:

- Native output is copied before `moonshine_free_embedding` and freed once.
- Empty text, Unicode text, dimension mismatch, empty vectors, closed model,
  and native errors.
- Native same-sentence similarity exceeds unrelated-sentence similarity.

### 16. Embedding manifest and catalog

Public API:

```go
func EmbeddingDependencies(model, variant string, options ...Option) (DownloadManifest, error)
func EmbeddingCatalog() (EmbeddingCatalogData, error)
```

Native symbols:

- `moonshine_get_embedding_dependencies`
- `moonshine_get_embedding_catalog`
- `moonshine_free_buffer`

Tests follow the string ownership, JSON, and native catalog tests from the STT
manifest increment.

### 17. Grapheme-to-phonemizer lifecycle

Public API:

```go
func NewPhonemizerFromFiles(language string, files []string, options ...Option) (*Phonemizer, error)
func NewPhonemizerFromMemory(language string, files map[string][]byte, options ...Option) (*Phonemizer, error)
func (p *Phonemizer) Close() error
```

Native symbols:

- `moonshine_create_grapheme_to_phonemizer_from_files`
- `moonshine_create_grapheme_to_phonemizer_from_memory`
- `moonshine_free_grapheme_to_phonemizer`

Tests mirror the standard lifecycle matrix, plus language/file ordering and
buffer-lifetime coverage.

### 18. Text to phonemes

Public API:

```go
func (p *Phonemizer) Phonemes(text string, options ...Option) (string, error)
```

Native symbols:

- `moonshine_text_to_phonemes`
- `moonshine_free_buffer`

Tests:

- Copy and free returned IPA exactly once.
- Empty/Unicode input, closed owner, options, nil output, and native errors.
- Native known-language pronunciation fixture aligned with Python/Swift.

### 19. G2P dependency manifest

Public API:

```go
func G2PDependencies(languages []string, options ...Option) (DownloadManifest, error)
```

Native symbols: `moonshine_get_g2p_dependencies`, `moonshine_free_buffer`.

Tests cover language joining, generic options, JSON decoding, ownership, known
languages, and invalid language errors.

### 20. Text-to-speech lifecycle

Public API:

```go
func NewTextToSpeechFromFiles(language string, files []string, options ...Option) (*TextToSpeech, error)
func NewTextToSpeechFromMemory(language string, files map[string][]byte, options ...Option) (*TextToSpeech, error)
func (t *TextToSpeech) Close() error
```

Native symbols:

- `moonshine_create_tts_synthesizer_from_files`
- `moonshine_create_tts_synthesizer_from_memory`
- `moonshine_free_tts_synthesizer`

Tests mirror lifecycle and memory-file coverage from transcriber/phonemizer,
including invalid language and missing-voice errors.

### 21. Speech synthesis

Public API:

```go
type Audio struct { Samples []float32; SampleRate int }
func (t *TextToSpeech) Synthesize(text string, options ...Option) (Audio, error)
func (t *TextToSpeech) SynthesizePhonemes(phonemes string, options ...Option) (Audio, error)
```

Native symbols:

- `moonshine_text_to_speech`
- `moonshine_phonemes_to_speech`
- `moonshine_free_buffer`

Tests:

- Copy samples before freeing the C buffer; free exactly once on every success.
- Empty text/phonemes, speed option, closed owner, invalid output rate/count,
  nil output, and native errors.
- Native output has samples, a positive sample rate, finite values, and expected
  broad duration bounds.
- Text-to-phonemes followed by phonemes-to-speech is behaviorally equivalent
  to direct text-to-speech within a documented tolerance.

### 22. Speech clip extraction

Public API:

```go
type SpeechClip struct { Audio Audio; Start, Duration float32; Complete bool; Transcript string }
func (t *TextToSpeech) ExtractSpeechClip(audio []float32, sampleRate int, options ...Option) (SpeechClip, error)
```

Native type/symbol: `moonshine_speech_clip_t`,
`moonshine_extract_speech_clip`.

Tests copy all nested data, cover empty/no-speech/partial/complete clips, options,
closed TTS owner, and a real checked-in voice-clone fixture.

### 23. TTS dependencies and voices

Public API:

```go
func TTSDependencies(languages []string, options ...Option) (DownloadManifest, error)
func TTSVoices(languages []string, options ...Option) (VoiceCatalog, error)
```

Native symbols:

- `moonshine_get_tts_dependencies`
- `moonshine_get_tts_voices`
- `moonshine_free_buffer`

Tests cover C-buffer ownership, JSON evolution, language/voice selection, found
versus missing state, invalid languages, and native catalog parity.

### 24. Higher-level voice clone workflow

This composes speech clip extraction, TTS manifests, and TTS construction. It
does not add raw bindings.

Tests should mirror existing `VoiceClone` tests: retained synthesizer ownership,
automatic transcript/refinement paths, explicit close, and representative
clone output behind the large-asset integration gate.

### 25. Microphone and agent conveniences

Only after the deterministic library APIs are complete:

- `MicTranscriber` with an injected capture backend and context cancellation.
- Event/listener convenience APIs.
- `AgentFlow` composition with STT, TTS, and embeddings.

Tests mirror existing API/threading suites: construction has no side effects,
load is explicit/idempotent, configuration passes through, capture callbacks
are never blocked by inference, handlers can be added/removed safely, owned
versus borrowed resources close correctly, and cancellation terminates all
goroutines.

## Complete C symbol coverage map

| C API | Public Go destination |
|---|---|
| `moonshine_get_version` | `Version` |
| `moonshine_error_to_string` | typed error conversion |
| `moonshine_free_buffer` | internal ownership helper only |
| `moonshine_transcript_to_string` | `Transcript.String` diagnostic path |
| `moonshine_load_transcriber_from_files` | `NewTranscriber` |
| `moonshine_load_transcriber_from_memory` | raw only; deprecated |
| `moonshine_load_transcriber_from_memory_files` | `NewTranscriberFromMemory` |
| `moonshine_free_transcriber` | `Transcriber.Close` |
| `moonshine_transcribe_without_streaming` | `Transcriber.Transcribe` |
| `moonshine_create_stream` | `Transcriber.NewStream` |
| `moonshine_free_stream` | `Stream.Close` |
| `moonshine_start_stream` | `Stream.Start` |
| `moonshine_stop_stream` | `Stream.Stop` |
| `moonshine_transcribe_add_audio_to_stream` | `Stream.AddAudio` |
| `moonshine_transcribe_stream` | `Stream.Transcript` |
| `moonshine_create_embedding_model` | `NewEmbeddingModel` |
| `moonshine_create_embedding_model_from_memory` | `NewEmbeddingModelFromMemory` |
| `moonshine_free_embedding_model` | `EmbeddingModel.Close` |
| `moonshine_calculate_embedding` | `EmbeddingModel.Embed` |
| `moonshine_free_embedding` | internal ownership helper only |
| `moonshine_calculate_embedding_distance` | `EmbeddingModel.Similarity` |
| `moonshine_extract_speech_clip` | `TextToSpeech.ExtractSpeechClip` |
| `moonshine_create_tts_synthesizer_from_files` | `NewTextToSpeechFromFiles` |
| `moonshine_create_tts_synthesizer_from_memory` | `NewTextToSpeechFromMemory` |
| `moonshine_free_tts_synthesizer` | `TextToSpeech.Close` |
| `moonshine_get_g2p_dependencies` | `G2PDependencies` |
| `moonshine_get_tts_dependencies` | `TTSDependencies` |
| `moonshine_get_tts_voices` | `TTSVoices` |
| `moonshine_get_stt_dependencies` | `STTDependencies` |
| `moonshine_get_embedding_dependencies` | `EmbeddingDependencies` |
| `moonshine_get_diarization_dependencies` | `DiarizationDependencies` |
| `moonshine_get_stt_catalog` | `STTCatalog` |
| `moonshine_get_embedding_catalog` | `EmbeddingCatalog` |
| `moonshine_text_to_speech` | `TextToSpeech.Synthesize` |
| `moonshine_phonemes_to_speech` | `TextToSpeech.SynthesizePhonemes` |
| `moonshine_create_grapheme_to_phonemizer_from_files` | `NewPhonemizerFromFiles` |
| `moonshine_create_grapheme_to_phonemizer_from_memory` | `NewPhonemizerFromMemory` |
| `moonshine_free_grapheme_to_phonemizer` | `Phonemizer.Close` |
| `moonshine_text_to_phonemes` | `Phonemizer.Phonemes` |

## Loose commit guide

This is the expected commit order, not a rigid contract. A commit may be split
when reviewability demands it, but unrelated APIs must not be combined merely
to follow the list.

1. `test(go): verify generated raw ABI`
2. `build(go): check deterministic binding generation`
3. `feat(go): add typed native errors`
4. `feat(go): add native option conversion`
5. `feat(go): complete transcriber constructors`
6. `test(go): add native transcriber lifecycle coverage`
7. `feat(go): expose library version`
8. `feat(go): add transcript value types`
9. `feat(go): add non-streaming transcription`
10. `test(go): mirror native non-streaming transcription cases`
11. `test(go): add shared WAV fixture loader`
12. `feat(go): add stream lifecycle`
13. `test(go): mirror native stream lifecycle cases`
14. `feat(go): add streaming audio and snapshots`
15. `test(go): mirror native streaming transcription cases`
16. `feat(go): add transcript events`
17. `feat(go): add adaptive stream update cadence`
18. `test(go): mirror stream cadence and latency cases`
19. `feat(go): add STT and diarization manifests`
20. `feat(go): add STT catalog`
21. `feat(go): add asset downloader`
22. `feat(go): add model cache`
23. `feat(go): add embedding model lifecycle`
24. `feat(go): add embedding calculation`
25. `feat(go): add embedding similarity`
26. `test(go): mirror native embedding cases`
27. `feat(go): add embedding manifest and catalog`
28. `feat(go): add phonemizer lifecycle`
29. `feat(go): add text to phonemes`
30. `test(go): mirror native phonemizer cases`
31. `feat(go): add G2P dependencies`
32. `feat(go): add text-to-speech lifecycle`
33. `feat(go): add text and phoneme synthesis`
34. `test(go): mirror native text-to-speech cases`
35. `feat(go): add speech clip extraction`
36. `feat(go): add TTS dependencies and voices`
37. `feat(go): add voice clone workflow`
38. `test(go): mirror native voice clone cases`
39. `feat(go): add microphone transcriber`
40. `test(go): mirror microphone API and threading cases`
41. `feat(go): add agent flow`
42. `test(go): mirror agent flow cases`
43. `docs(go): add binding examples and API guide`
44. `ci(go): run generation and binding parity tests`
45. `test(go): close Swift and Python parity gaps`

## Definition of complete

The Go binding is complete when:

- Every non-deprecated C symbol appears in the coverage map and is exercised by
  at least one Go test.
- Every C allocation has a documented owner and a test proving exactly-once
  release.
- Public APIs expose no cgo types or native pointers.
- Native integration coverage matches the relevant Python, Swift, and Android
  behavior for transcription, streaming, embeddings, G2P, TTS, and catalogs.
- `go test -race` passes for unit tests that do not require the native linker,
  and the full `scripts/build-go.sh` suite passes on supported hosts.
- Generation is deterministic and CI fails on stale generated files.
- Package documentation includes a minimal STT example, streaming example,
  embedding example, G2P example, and TTS example, each with explicit cleanup.
