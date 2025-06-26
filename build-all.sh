#!/bin/bash

GO_FILE="src/rncs.go"
BIN_DIR="bin"

mkdir -p "$BIN_DIR"

echo "Building for Linux (amd64)..."
GOOS=linux GOARCH=amd64 go build -o "$BIN_DIR/rncs_linux" "$GO_FILE"

echo "Building for Windows (amd64)..."
GOOS=windows GOARCH=amd64 go build -o "$BIN_DIR/rncs_win.exe" "$GO_FILE"

echo "Building for macOS (amd64)..."
GOOS=darwin GOARCH=amd64 go build -o "$BIN_DIR/rncs_mac" "$GO_FILE"

echo "Building for macOS (ARM64)..."
GOOS=darwin GOARCH=arm64 go build -o "$BIN_DIR/rncs_mac_arm" "$GO_FILE"

echo "Building for Linux (ARM)..."
GOOS=linux GOARCH=arm go build -o "$BIN_DIR/rncs_arm" "$GO_FILE"

echo "✅ All builds complete. The binaries are in the $BIN_DIR folder."
