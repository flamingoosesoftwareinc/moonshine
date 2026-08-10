# Moonshine Go bindings

The `moonshine` package is the public, ownership-safe API. It provides
transcription, streaming events, embeddings, G2P, speech synthesis, verified
asset downloads, microphone orchestration, and AgentFlow composition. Native
handles implement idempotent `Close`; returned transcripts, vectors, strings,
audio, manifests, and catalogs are copied into Go-owned memory.

The `raw` package is intentionally mechanical and should remain behind small,
mockable interfaces in the public package.

The `raw` package is generated from `core/moonshine-c-api.h` with
[`c-for-go`](https://github.com/xlab/c-for-go). Do not edit its generated files
directly.

Generate the bindings from this directory:

```sh
go generate ./raw
```

Verify that committed generated files match the manifest without rewriting the
working tree:

```sh
../../scripts/check-go-generated.sh
```

The generated package includes and links the in-tree Moonshine core. Build the
native library before linking tests or applications:

```sh
cmake -S ../../core -B ../../core/build
cmake --build ../../core/build
go test ./...
```

The canonical build script compiles the native library, verifies deterministic
generation, configures the ONNX Runtime loader path, and runs model-free tests:

```sh
../../scripts/build-go.sh
```

Model-backed suites are explicit and fetch only their documented fixtures into
gitignored asset directories:

```sh
../../scripts/build-go.sh integration
../../scripts/build-go.sh g2p-integration
../../scripts/build-go.sh tts-integration
../../scripts/build-go.sh embedding-integration
../../scripts/build-go.sh roundtrip
```

`roundtrip` synthesizes speech, writes a WAV under the temporary directory,
and transcribes that same audio. The other modes run focused native parity
tests. None of these downloads occur during ordinary `go test`.

## Public API examples

Compilable package examples cover:

- non-streaming transcription and explicit transcriber cleanup;
- streaming events, cadence, listener removal, and stream cleanup;
- embedding calculation and native similarity;
- text-to-IPA conversion;
- text-to-speech synthesis.

Run them as part of the normal suite, or browse `moonshine/examples_test.go`.
The `examples/voice-roundtrip` command is the opt-in end-to-end confidence
demo discussed above.

## Ownership and errors

- Close every `Transcriber`, `Stream`, `EmbeddingModel`, `Phonemizer`, and
  `TextToSpeech` explicitly.
- Memory-backed constructors retain and pin supplied buffers until `Close`; do
  not modify them while the owner is alive.
- Use `errors.Is` with exported sentinels such as `ErrInvalidArgument`,
  `ErrInvalidHandle`, `ErrClosed`, and `ErrInvalidNativeOutput`.
- `MicTranscriberConfig` and `AgentFlowConfig` state whether injected resources
  are borrowed or transferred to the wrapper.
- Public APIs expose no cgo type or native pointer.

`c-for-go.yml` is the source of truth for symbol selection, naming, and pointer
hints required by the C API.
