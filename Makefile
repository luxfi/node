# Lux Node Makefile

.PHONY: all build test clean lint fmt release

# Variables
GOPATH ?= $(shell go env GOPATH)
GOBIN ?= $(GOPATH)/bin
VERSION ?= $(shell git describe --tags --always --dirty)
COMMIT ?= $(shell git rev-parse HEAD)
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Build flags
LDFLAGS = -ldflags "\
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)"

# Default target
all: build

# Build the main binary
build:
	@echo "Building luxd..."
	@./scripts/build.sh
	@echo "✓ Build complete: ./build/luxd"

# Run tests
test:
	@echo "Running tests..."
	@go test -v -short -timeout 30m ./...
	@echo "✓ Tests complete"

# Run quick tests (core packages only)
test-quick:
	@echo "Running quick tests..."
	@go test -short -timeout 5m \
		./utils/... \
		./vms/components/... \
		./network/... \
		./api/...
	@echo "✓ Quick tests complete"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf build/
	@go clean -cache
	@echo "✓ Clean complete"

# Run linter
lint:
	@echo "Running linter..."
	@golangci-lint run --timeout 10m
	@echo "✓ Lint complete"

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@gofmt -s -w .
	@echo "✓ Format complete"

# Install dependencies
deps:
	@echo "Installing dependencies..."
	@go mod download
	@go mod tidy
	@echo "✓ Dependencies installed"

# Build release binaries
release: clean
	@echo "Building release binaries..."
	@./scripts/build.sh
	@mkdir -p dist
	@tar -czf dist/luxd-$(VERSION)-linux-amd64.tar.gz build/luxd
	@echo "✓ Release build complete: dist/luxd-$(VERSION)-linux-amd64.tar.gz"

# Run CI checks locally
ci: fmt lint test build
	@echo "✓ All CI checks passed!"

# Show version
version:
	@./build/luxd --version

# Help
help:
	@echo "Available targets:"
	@echo "  make build      - Build the luxd binary"
	@echo "  make test       - Run all tests"
	@echo "  make test-quick - Run quick tests (core packages)"
	@echo "  make clean      - Clean build artifacts"
	@echo "  make lint       - Run linter"
	@echo "  make fmt        - Format code"
	@echo "  make deps       - Install dependencies"
	@echo "  make release    - Build release binaries"
	@echo "  make ci         - Run all CI checks"
	@echo "  make version    - Show version"
	@echo "  make help       - Show this help"