#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

# Build Linux and Windows agent
mkdir -p dist

GOFLAGS="-trimpath"
LDFLAGS="-s -w"

echo "Building agent for linux/amd64..."
GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o dist/agent-linux-amd64 ./agent

echo "Building agent for windows/amd64..."
GOOS=windows GOARCH=amd64 go build -ldflags "$LDFLAGS" -o dist/agent-windows-amd64.exe ./agent
