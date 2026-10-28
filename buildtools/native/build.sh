#!/usr/bin/env bash
# Builds the same pinned PCRE2 release as the wasm2go backend, natively and
# with the JIT, for the cgo backend (-tags pcre2_cgo). Installs a static
# library under PREFIX and prints the PKG_CONFIG_PATH to use.
#
#   PREFIX=$HOME/.local/pcre2 buildtools/native/build.sh
#   export PKG_CONFIG_PATH=$HOME/.local/pcre2/lib/pkgconfig
#   go test -tags pcre2_cgo ./...
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
# shellcheck source=../wasm/versions.env
. "$root/buildtools/wasm/versions.env"
prefix="${PREFIX:?set PREFIX to the install directory}"
src="$(mktemp -d)"
trap 'rm -rf "$src"' EXIT

git clone --quiet --depth 1 --branch "$PCRE2_TAG" https://github.com/PCRE2Project/pcre2 "$src"
test "$(git -C "$src" rev-parse HEAD)" = "$PCRE2_COMMIT"
# The JIT lives in the sljit submodule.
git -C "$src" submodule update --init --depth 1 deps/sljit

cmake -S "$src" -B "$src/build" \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_INSTALL_PREFIX="$prefix" \
  -DCMAKE_INSTALL_LIBDIR=lib \
  -DBUILD_SHARED_LIBS=OFF \
  -DBUILD_STATIC_LIBS=ON \
  -DPCRE2_BUILD_PCRE2_8=ON \
  -DPCRE2_SUPPORT_JIT=ON \
  -DPCRE2_SUPPORT_UNICODE=ON \
  -DPCRE2_BUILD_PCRE2GREP=OFF \
  -DPCRE2_BUILD_TESTS=OFF
cmake --build "$src/build" -j"$(nproc)"
cmake --install "$src/build"

echo "PKG_CONFIG_PATH=$prefix/lib/pkgconfig"
