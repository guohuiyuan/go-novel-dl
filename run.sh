#!/usr/bin/env bash

set -euo pipefail

echo "========================================"
echo "Novel DL - Build And Run (Linux/macOS)"
echo "========================================"

echo "Checking Go environment..."
go version
go env -w GOPROXY=https://goproxy.cn,direct
go env -w GO111MODULE=on

echo "Tidying modules..."
go mod tidy

echo "Building novel-dl..."
go build -ldflags="-s -w" -o novel-dl ./cmd/novel-dl

echo
echo "Usage:"
echo "  1. Interactive CLI: ./novel-dl"
echo "  2. Search by keyword: ./novel-dl \"三体\""
echo "  3. Start Web UI: ./novel-dl web --no-browser"
echo

./novel-dl
