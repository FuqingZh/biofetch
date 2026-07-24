#!/usr/bin/env bash

set -euo pipefail

dir_script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
dir_repo="$(cd "${dir_script}/.." && pwd)"

go_bin="${GO_BIN:-$(command -v go)}"
go_cache="${GOCACHE:-/tmp/biofetch-go-build}"
file_out="${OUTPUT_BIN:-${dir_repo}/dist/biofetch}"
version_build="${VERSION_BUILD:-$(git -C "${dir_repo}" describe --tags --always --dirty)}"

mkdir -p "$(dirname "${file_out}")"

cd "${dir_repo}"
env GOCACHE="${go_cache}" CGO_ENABLED=0 "${go_bin}" build \
  -ldflags "-X biofetch/internal/biofetch.Version=${version_build}" \
  -o "${file_out}" ./cmd/biofetch
