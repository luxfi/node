# Makefile for luxfi/database module

.PHONY: all build test clean fmt lint install-tools test-coverage test-integration

# Variables
GOBIN := $(shell go env GOPATH)/bin
GOLANGCI_LINT_VERSION := v1.54.2
COVERAGE_FILE := coverage.out
COVERAGE_HTML := coverage.html

# Default target
all: clean fmt test build

# Build the module
build:
	@echo "Building database module..."
	@go build -v ./...

# Run tests
test:
	@echo "Running tests..."
	@go test -v -race -timeout=10m ./...

# Run integration tests
test-integration:
	@echo "Running integration tests..."
	@go test -v -race -timeout=30m -tags=integration ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test -v -race -timeout=10m -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
	@go tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Coverage report generated: $(COVERAGE_HTML)"

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@go mod tidy

# Run linter
lint: install-tools
	@echo "Running linter..."
	@$(GOBIN)/golangci-lint run ./...

# Install development tools
install-tools:
	@echo "Installing development tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@go clean -cache -testcache
	@rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	@go test -bench=. -benchmem ./...

# Run database-specific benchmarks
bench-db:
	@echo "Running database benchmarks..."
	@go test -bench=. -benchmem ./dbtest

# Check for security vulnerabilities
security:
	@echo "Checking for vulnerabilities..."
	@go list -json -m all | nancy sleuth

# Update dependencies
deps:
	@echo "Updating dependencies..."
	@go get -u -t ./...
	@go mod tidy

# Verify module
verify:
	@echo "Verifying module..."
	@go mod verify

# Generate mocks
mocks:
	@echo "Generating mocks..."
	@go generate ./...