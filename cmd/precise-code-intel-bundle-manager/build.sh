#!/usr/bin/env bash

# This script builds the precise-code-intel-bundle-manager docker image.

cd "$(dirname "${BASH_SOURCE[0]}")/../.."
set -eux

OUTPUT=`mktemp -d -t sgdockerbuild_XXXXXXX`
cleanup() {
    rm -rf "$OUTPUT"
}
trap cleanup EXIT

# Environment for building linux binaries
export GO111MODULE=on
export GOARCH=amd64
export GOOS=linux
export OUTPUT # build artifact goes here
./cmd/precise-code-intel-bundle-manager/go-build.sh

cp -a ./dev/libsqlite3-pcre/install-alpine.sh "$OUTPUT/libsqlite3-pcre-install-alpine.sh"

echo "--- docker build"
docker build -f cmd/precise-code-intel-bundle-manager/Dockerfile -t "$IMAGE" "$OUTPUT" \
    --progress=plain \
    ${DOCKER_BUILD_FLAGS:-} \
    --build-arg COMMIT_SHA \
    --build-arg DATE \
    --build-arg VERSION
