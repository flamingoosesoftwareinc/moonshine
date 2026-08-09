# Moonshine Go bindings

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

`c-for-go.yml` is the source of truth for symbol selection, naming, and pointer
hints required by the C API.
