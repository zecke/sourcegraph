#!/usr/bin/env bash

# This script builds the ctags image and the precise-code-intel-bundle-manager go binary, then runs the go binary.

cd "$(dirname "${BASH_SOURCE[0]}")/../.."
set -eu

# Build and run precise-code-intel-bundle-manager binary
./dev/libsqlite3-pcre/build.sh
OUTPUT=./.bin ./cmd/precise-code-intel-bundle-manager/go-build.sh
LIBSQLITE3_PCRE="$(./dev/libsqlite3-pcre/build.sh libpath)" ./.bin/precise-code-intel-bundle-manager
