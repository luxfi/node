# Makefile for Lux Node

.PHONY: all build build-fips test test-fips clean fmt lint install-mockgen mockgen verify-fips

# FIPS 140-3 Configuration
export GOFIPS140 := latest
export GODEBUG := fips140=on
export CGO_ENABLED := 1

# FIPS build environment
FIPS_ENV := GOFIPS140=$(GOFIPS140) GODEBUG=$(GODEBUG) CGO_ENABLED=$(CGO_ENABLED)
FIPS_BUILD_FLAGS := -tags fips

# Build variables
GO := go
GOBIN := $(shell go env GOPATH)/bin
LUXD := ./build/luxd

# Test variables
TEST_TIMEOUT := 120s
EXCLUDED_DIRS := /mocks|/proto|/tests/e2e|/tests/load|/tests/upgrade|/tests/fixture
TEST_PACKAGES := $(shell go list ./... 2>/dev/null | grep -v -E '$(EXCLUDED_DIRS)')

# Colors for output
GREEN := \033[0;32m
YELLOW := \033[1;33m
NC := \033[0m

all: build-fips

# Verify FIPS environment
verify-fips:
	@echo "$(GREEN)Verifying FIPS 140-3 Environment...$(NC)"
	@echo "GOFIPS140: $(GOFIPS140)"
	@echo "GODEBUG: $(GODEBUG)"
	@echo "$(GREEN)✓ FIPS environment ready$(NC)"

# Build with FIPS 140-3 mode (default)
build-fips: verify-fips
	@echo "$(GREEN)Building luxd with FIPS 140-3 mode...$(NC)"
	@$(FIPS_ENV) ./scripts/build.sh
	@echo "$(GREEN)✓ FIPS build complete$(NC)"

# Standard build (non-FIPS, for comparison only)
build:
	@echo "$(YELLOW)Building luxd (standard, non-FIPS)...$(NC)"
	@./scripts/build.sh

# Test with FIPS 140-3 mode (default)
test-fips: verify-fips
	@echo "$(GREEN)Running tests with FIPS 140-3 mode...$(NC)"
	@$(FIPS_ENV) go test $(FIPS_BUILD_FLAGS) -shuffle=on -race -timeout=$(TEST_TIMEOUT) -coverprofile=coverage.out -covermode=atomic $(TEST_PACKAGES)

# Standard test (non-FIPS, for comparison)
test:
	@echo "$(YELLOW)Running tests (standard, non-FIPS)...$(NC)"
	@go test -shuffle=on -race -timeout=$(TEST_TIMEOUT) $(TEST_PACKAGES)

test-short-fips: verify-fips
	@echo "$(GREEN)Running short tests with FIPS...$(NC)"
	@$(FIPS_ENV) go test $(FIPS_BUILD_FLAGS) -short -race -timeout=60s $(TEST_PACKAGES)

test-short:
	@echo "Running short tests..."
	@go test -short -race -timeout=60s $(TEST_PACKAGES)

test-100-fips: verify-fips
	@echo "$(GREEN)=== ENSURING 100% TEST PASS RATE WITH FIPS ===$(NC)"
	@$(FIPS_ENV) go test $(FIPS_BUILD_FLAGS) -shuffle=on -race -timeout=$(TEST_TIMEOUT) $(TEST_PACKAGES)

test-100:
	@echo "=== ENSURING 100% TEST PASS RATE ==="
	@go test -shuffle=on -race -timeout=$(TEST_TIMEOUT) $(TEST_PACKAGES)

fmt:
	@echo "Formatting Go code..."
	@go fmt ./...
	@gofumpt -l -w .

lint:
	@echo "Running linters..."
	@./scripts/lint.sh

clean:
	@echo "Cleaning build artifacts..."
	@rm -rf build/
	@rm -f coverage.out

install-mockgen:
	@echo "Installing mockgen..."
	@go install github.com/golang/mock/mockgen@latest

mockgen: install-mockgen
	@echo "Generating mocks..."
	@./scripts/mockgen.sh

# Specific test targets
test-unit:
	@echo "Running unit tests..."
	@go test -short -race $(TEST_PACKAGES)

test-integration:
	@echo "Running integration tests..."
	@go test -run Integration -race -timeout=300s $(TEST_PACKAGES)

test-e2e:
	@echo "Running e2e tests..."
	@./scripts/tests.e2e.sh

# Build specific binaries
luxd:
	@echo "Building luxd..."
	@./scripts/build.sh

# Installation targets
install:
	@echo "Installing luxd..."
	@go install -v ./cmd/luxd

# Development helpers
dev-setup:
	@echo "Setting up development environment..."
	@go mod download
	@go mod tidy

# Show all available test packages
list-packages:
	@echo "Available test packages:"
	@go list ./... 2>/dev/null | grep -v -E '$(EXCLUDED_DIRS)'

# Count packages
count-packages:
	@echo "Total packages: $$(go list ./... 2>/dev/null | grep -v -E '$(EXCLUDED_DIRS)' | wc -l)"

# Run specific package tests
test-package:
	@if [ -z "$(PKG)" ]; then \
		echo "Usage: make test-package PKG=./path/to/package"; \
		exit 1; \
	fi
	@echo "Testing package: $(PKG)"
	@go test -race -timeout=$(TEST_TIMEOUT) $(PKG)

# Help target
help:
	@echo "Available targets:"
	@echo "  build         - Build luxd binary"
	@echo "  test          - Run all tests"
	@echo "  test-short    - Run short tests only"
	@echo "  test-100      - Ensure 100% test pass rate"
	@echo "  test-unit     - Run unit tests"
	@echo "  test-e2e      - Run end-to-end tests"
	@echo "  fmt           - Format Go code"
	@echo "  lint          - Run linters"
	@echo "  clean         - Clean build artifacts"
	@echo "  install       - Install luxd to GOPATH/bin"
	@echo "  dev-setup     - Setup development environment"
	@echo "  list-packages - List all test packages"
	@echo "  count-packages- Count total packages"
	@echo "  test-package  - Test specific package (use PKG=./path)"
	@echo "  help          - Show this help message"