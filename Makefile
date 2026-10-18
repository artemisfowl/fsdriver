# Name of the output binary
BINARY_NAME = fsdriver
MAIN_PATH = ./cmd/fsdriver

# Automatic versioning using Git
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")

# Linker flags to inject variables into the binary
LDFLAGS = -ldflags "-s -w -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.Date=$(DATE)"

.PHONY = all build clean version

# Default target: builds native binary with version flags
build:
	@echo "Building $(BINARY_NAME)... [Version: $(VERSION), Commit: $(COMMIT)]"
	mkdir -p build
	go build $(LDFLAGS) -o build/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Native binary built: build/$(BINARY_NAME)"

# Cross-compiles for all major platforms with version injection
all: clean
	@echo "Building binaries for all platforms..."
	mkdir -p build

	# Linux
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o build/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o build/$(BINARY_NAME)-linux-arm64 $(MAIN_PATH)

	# macOS
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o build/$(BINARY_NAME)-darwin-amd64 $(MAIN_PATH)
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o build/$(BINARY_NAME)-darwin-arm64 $(MAIN_PATH)

	# Windows
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o build/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PATH)
	GOOS=windows GOARCH=arm64 go build $(LDFLAGS) -o build/$(BINARY_NAME)-windows-arm64.exe $(MAIN_PATH)

	@echo "All binaries successfully built inside the 'build/' directory."

# Helper target to check current version string
version:
	@echo "Version: $(VERSION)"
	@echo "Commit:  $(COMMIT)"
	@echo "Date:    $(DATE)"

# Clean build artifacts
clean:
	@echo "Cleaning build directory..."
	rm -rf build
