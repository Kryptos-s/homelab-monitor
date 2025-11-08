#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

mkdir -p dist

LDFLAGS="-s -w"

echo "Building dashboard for linux/amd64..."
GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o dist/dashboard-linux-amd64 ./dashboard

echo "Building dashboard for windows/amd64..."
GOOS=windows GOARCH=amd64 go build -ldflags "$LDFLAGS" -o dist/dashboard-windows-amd64.exe ./dashboard
