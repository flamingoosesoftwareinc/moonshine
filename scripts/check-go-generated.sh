#!/bin/bash -e

SCRIPTS_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT_DIR="$(dirname "${SCRIPTS_DIR}")"
GO_DIR="${REPO_ROOT_DIR}/language-bindings/go"
RAW_DIR="${GO_DIR}/raw"
WORK_DIR="$(mktemp -d)"

cleanup() {
	rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

cd "${RAW_DIR}"
go run github.com/xlab/c-for-go@v1.3.0 \
	-nostamp \
	-out "${WORK_DIR}" \
	../c-for-go.yml

GENERATED_FILES=(
	cgo_helpers.go
	cgo_helpers.h
	const.go
	doc.go
	raw.go
	types.go
)

for generated_file in "${GENERATED_FILES[@]}"; do
	diff -u \
		"${RAW_DIR}/${generated_file}" \
		"${WORK_DIR}/raw/${generated_file}"
done
