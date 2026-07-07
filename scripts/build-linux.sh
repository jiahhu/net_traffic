#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
mkdir -p dist
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -X main.version=$VERSION" \
  -o dist/nettraffic-linux-amd64 ./cmd/nettraffic
echo "created dist/nettraffic-linux-amd64 ($VERSION)"

