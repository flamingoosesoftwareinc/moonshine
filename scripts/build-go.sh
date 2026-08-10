#!/bin/bash -ex

SCRIPTS_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT_DIR="$(dirname "${SCRIPTS_DIR}")"
CORE_DIR="${REPO_ROOT_DIR}/core"
CORE_BUILD_DIR="${CORE_DIR}/build"
GO_DIR="${REPO_ROOT_DIR}/language-bindings/go"

GO_TEST_ARGS=()
case "${1:-}" in
	"") ;;
	integration)
		# Model-backed parity tests are opt-in. The fetch is idempotent and
		# downloads only the Tiny English fixture used by Swift and Android.
		"${SCRIPTS_DIR}/fetch-voice-assets.sh" tiny-en
		GO_TEST_ARGS+=("-tags=integration")
		;;
	roundtrip)
		# Explicit model-backed synthesis -> WAV -> transcription confidence
		# example. Keep its larger TTS assets out of every test suite.
		"${SCRIPTS_DIR}/fetch-voice-assets.sh" tiny-en
		"${SCRIPTS_DIR}/fetch-voice-assets.sh" tts-smoke
		;;
	embedding-integration)
		# Explicit large-model test. The Go test resolves and downloads the
		# embedding manifest through the public downloader API.
		GO_TEST_ARGS+=("-tags=integration,embedding_integration")
		;;
	*)
		echo "usage: $0 [integration|roundtrip|embedding-integration]" >&2
		exit 2
		;;
esac

# Generate the host-native shared library consumed by cgo. Building only the
# moonshine target avoids compiling the core test executables for a binding
# build.
cmake -S "${CORE_DIR}" -B "${CORE_BUILD_DIR}" -DCMAKE_BUILD_TYPE=Release
cmake --build "${CORE_BUILD_DIR}" --config Release --target moonshine

cd "${GO_DIR}"
"${SCRIPTS_DIR}/check-go-generated.sh"

# The Go test binary needs to find libmoonshine and Moonshine's vendored ONNX
# Runtime dependency. Select the latter using the same host mapping as the
# other native build scripts.
UNAME_S="$(uname -s)"
UNAME_M="$(uname -m)"
ORT_LIB_ROOT="${CORE_DIR}/third-party/onnxruntime/lib"

if [[ "${UNAME_S}" == "Darwin" ]]; then
	export DYLD_LIBRARY_PATH="${CORE_BUILD_DIR}:${ORT_LIB_ROOT}/macos/${UNAME_M}:${DYLD_LIBRARY_PATH:-}"
elif [[ "${UNAME_S}" == "Linux" ]]; then
	if [[ "${UNAME_M}" == "aarch64" || "${UNAME_M}" == "arm64" ]]; then
		ORT_ARCH_DIR="${ORT_LIB_ROOT}/linux/aarch64"
	else
		ORT_ARCH_DIR="${ORT_LIB_ROOT}/linux/x86_64"
	fi
	export LD_LIBRARY_PATH="${CORE_BUILD_DIR}:${ORT_ARCH_DIR}:${LD_LIBRARY_PATH:-}"
fi

go test "${GO_TEST_ARGS[@]}" ./...

if [[ "${1:-}" == "roundtrip" ]]; then
	go run ./examples/voice-roundtrip \
		-stt-model "${REPO_ROOT_DIR}/test-assets/tiny-en" \
		-tts-root "${CORE_DIR}/moonshine-tts/data" \
		-output "${TMPDIR:-/tmp}/moonshine-voice-roundtrip.wav"
fi
