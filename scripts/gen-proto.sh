#!/usr/bin/env sh
set -eu

if ! command -v buf >/dev/null 2>&1; then
  echo "buf is not installed; cannot generate protobuf code" >&2
  exit 1
fi

if ! command -v protoc-gen-go >/dev/null 2>&1; then
  echo "protoc-gen-go is not installed; cannot generate protobuf code" >&2
  exit 1
fi

if ! command -v protoc-gen-go-grpc >/dev/null 2>&1; then
  echo "protoc-gen-go-grpc is not installed; cannot generate protobuf code" >&2
  exit 1
fi

buf generate
