# Makefile for wsh - WebSocket Shell
# Cross-platform build targets for Linux, macOS, and Windows

# Binary name
BINARY_NAME=wsh
MODULE_NAME=github.com/fabricates/wsh

# Version (can be overridden: make VERSION=1.0.0)
VERSION?=dev
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME?=$(shell date -u '+%Y-%m-%d_%H:%M:%S')

# Build flags
LDFLAGS=-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)

# Output directory
OUT_DIR=bin

.PHONY: all clean build linux darwin windows \
	linux-amd64 linux-arm64 \
	darwin-amd64 darwin-arm64 \
	windows-amd64 windows-arm64 \
	install test help

# Default target
all: clean build

# Build for current platform
build:
	@echo "Building for current platform..."
	@mkdir -p $(OUT_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY_NAME) .
	@echo "Build complete: $(OUT_DIR)/$(BINARY_NAME)"

# Install to GOPATH/bin
install:
	@echo "Installing $(BINARY_NAME)..."
	go install -ldflags "$(LDFLAGS)" .
	@echo "Installed to $(shell go env GOPATH)/bin/$(BINARY_NAME)"

# Build for all platforms
cross-compile: linux darwin windows
	@echo "Cross-compilation complete!"

# Linux builds
linux: linux-amd64 linux-arm64

linux-amd64:
	@echo "Building for Linux amd64..."
	@mkdir -p $(OUT_DIR)
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY_NAME)-linux-amd64 .

linux-arm64:
	@echo "Building for Linux arm64..."
	@mkdir -p $(OUT_DIR)
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY_NAME)-linux-arm64 .

# macOS builds
darwin: darwin-amd64 darwin-arm64

darwin-amd64:
	@echo "Building for macOS amd64..."
	@mkdir -p $(OUT_DIR)
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY_NAME)-darwin-amd64 .

darwin-arm64:
	@echo "Building for macOS arm64 (Apple Silicon)..."
	@mkdir -p $(OUT_DIR)
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY_NAME)-darwin-arm64 .

# Windows builds
windows: windows-amd64 windows-arm64

windows-amd64:
	@echo "Building for Windows amd64..."
	@mkdir -p $(OUT_DIR)
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY_NAME)-windows-amd64.exe .

windows-arm64:
	@echo "Building for Windows arm64..."
	@mkdir -p $(OUT_DIR)
	GOOS=windows GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY_NAME)-windows-arm64.exe .

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(OUT_DIR)
	@rm -f $(BINARY_NAME)
	@echo "Clean complete"

# Tidy dependencies
tidy:
	@echo "Tidying go modules..."
	go mod tidy

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Run linter (requires golangci-lint)
lint:
	@echo "Running linter..."
	@which golangci-lint > /dev/null || (echo "golangci-lint not found. Install: https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run

# Show help
help:
	@echo "Available targets:"
	@echo "  make build           - Build for current platform"
	@echo "  make install         - Install to GOPATH/bin"
	@echo "  make cross-compile   - Build for all platforms"
	@echo "  make linux           - Build for Linux (amd64, arm64)"
	@echo "  make darwin          - Build for macOS (amd64, arm64)"
	@echo "  make windows         - Build for Windows (amd64, arm64)"
	@echo "  make linux-amd64     - Build for Linux amd64"
	@echo "  make linux-arm64     - Build for Linux arm64"
	@echo "  make darwin-amd64    - Build for macOS amd64"
	@echo "  make darwin-arm64    - Build for macOS arm64 (Apple Silicon)"
	@echo "  make windows-amd64   - Build for Windows amd64"
	@echo "  make windows-arm64   - Build for Windows arm64"
	@echo "  make test            - Run tests"
	@echo "  make clean           - Remove build artifacts"
	@echo "  make tidy            - Tidy go modules"
	@echo "  make fmt             - Format code"
	@echo "  make lint            - Run linter (requires golangci-lint)"
	@echo "  make help            - Show this help message"
	@echo ""
	@echo "Variables:"
	@echo "  VERSION=x.y.z        - Set version (default: dev)"
	@echo ""
	@echo "Examples:"
	@echo "  make build"
	@echo "  make cross-compile"
	@echo "  make VERSION=1.0.0 linux-amd64"
