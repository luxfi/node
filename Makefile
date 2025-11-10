# Makefile for Lux Node

.PHONY: all build build-cgo build-nocgo build-mlx test test-cgo test-nocgo clean fmt lint install-mockgen mockgen

# Configuration toggles
CGO ?= 0
FIPS_STRICT ?= 0

# FIPS 140-3 always enabled (required for blockchain/financial systems)
export GOFIPS140 := latest
ifeq ($(FIPS_STRICT),1)
	export GODEBUG := fips140=only
else
	export GODEBUG := fips140=on
endif

export CGO_ENABLED := $(CGO)

# Environment block for all go commands
ENV := GOFIPS140=$(GOFIPS140) GODEBUG=$(GODEBUG) CGO_ENABLED=$(CGO_ENABLED)

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

all: build

# Verify FIPS environment
verify-fips:
	@echo "$(GREEN)Verifying FIPS 140-3 Environment...$(NC)"
	@echo "FIPS: $(FIPS)"
	@echo "CGO: $(CGO)"
	@echo "FIPS_STRICT: $(FIPS_STRICT)"
	@echo "GOFIPS140: $(GOFIPS140)"
	@echo "GODEBUG: $(GODEBUG)"
	@echo "CGO_ENABLED: $(CGO_ENABLED)"
	@echo "$(GREEN)✓ Environment ready$(NC)"

# Default build (uses current FIPS and CGO settings)
build:
	@echo "$(GREEN)Building luxd (FIPS=$(FIPS) CGO=$(CGO))...$(NC)"
	@$(ENV) ./scripts/build.sh
	@echo "$(GREEN)✓ Build complete$(NC)"

# Convenience aliases
build-cgo:
	@$(MAKE) build CGO=1

build-nocgo:
	@$(MAKE) build CGO=0

build-fips:
	@$(MAKE) build FIPS=1

# Default test (uses current FIPS and CGO settings)
test:
	@echo "$(GREEN)Running tests (FIPS=$(FIPS) CGO=$(CGO))...$(NC)"
	@$(ENV) go test -shuffle=on -race -timeout=$(TEST_TIMEOUT) -coverprofile=coverage.out -covermode=atomic $(TEST_PACKAGES)

# Convenience aliases
test-cgo:
	@$(MAKE) test CGO=1

test-nocgo:
	@$(MAKE) test CGO=0

test-fips:
	@$(MAKE) test FIPS=1

test-short:
	@echo "Running short tests (FIPS=$(FIPS) CGO=$(CGO))..."
	@$(ENV) go test -short -race -timeout=60s $(TEST_PACKAGES)

test-100:
	@echo "$(GREEN)=== ENSURING 100% TEST PASS RATE (FIPS=$(FIPS) CGO=$(CGO)) ===$(NC)"
	@$(ENV) go test -shuffle=on -race -timeout=$(TEST_TIMEOUT) $(TEST_PACKAGES)

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
	@echo "Running unit tests (FIPS=$(FIPS) CGO=$(CGO))..."
	@$(ENV) go test -short -race $(TEST_PACKAGES)

test-integration:
	@echo "Running integration tests (FIPS=$(FIPS) CGO=$(CGO))..."
	@$(ENV) go test -run Integration -race -timeout=300s $(TEST_PACKAGES)

test-e2e:
	@echo "Running e2e tests (FIPS=$(FIPS) CGO=$(CGO))..."
	@$(ENV) ./scripts/tests.e2e.sh

# Build specific binaries
luxd:
	@echo "Building luxd (FIPS=$(FIPS) CGO=$(CGO))..."
	@$(ENV) ./scripts/build.sh

# Installation targets
install:
	@echo "Installing luxd (FIPS=$(FIPS) CGO=$(CGO))..."
	@$(ENV) go install -v ./cmd/luxd

# Development helpers
dev-setup:
	@echo "Setting up development environment..."
	@$(ENV) go mod download
	@$(ENV) go mod tidy

# Show all available test packages
list-packages:
	@echo "Available test packages:"
	@$(ENV) go list ./... 2>/dev/null | grep -v -E '$(EXCLUDED_DIRS)'

# Count packages
count-packages:
	@echo "Total packages: $$($(ENV) go list ./... 2>/dev/null | grep -v -E '$(EXCLUDED_DIRS)' | wc -l)"

# Run specific package tests
test-package:
	@if [ -z "$(PKG)" ]; then \
		echo "Usage: make test-package PKG=./path/to/package"; \
		exit 1; \
	fi
	@echo "Testing package: $(PKG) (FIPS=$(FIPS) CGO=$(CGO))"
	@$(ENV) go test -race -timeout=$(TEST_TIMEOUT) $(PKG)

# Node runtime targets
init-chains:
	@echo "$(GREEN)Initializing chain directory structure...$(NC)"
	@mkdir -p ./chains/{C,P,X,Q}/db
	@mkdir -p ./logs
	@echo "$(GREEN)✓ Chain directories created$(NC)"

migrate-chain-data: init-chains
	@echo "$(GREEN)Migrating existing chain data...$(NC)"
	@if [ -d "$(HOME)/.luxd/chainData/C/db" ]; then \
		cp -r $(HOME)/.luxd/chainData/C/db/* ./chains/C/db/ 2>/dev/null && \
		echo "$(GREEN)✓ C-chain data migrated$(NC)"; \
	fi

run-mainnet: build-fips init-chains
	@echo "$(GREEN)Starting Lux Mainnet (ID: 96369)...$(NC)"
	@pkill -f luxd || true
	@sleep 2
	$(LUXD) \
		--network-id=96369 \
		--staking-enabled=false \
		--http-host=0.0.0.0 \
		--http-port=9630 \
		--data-dir=./chains \
		--db-dir=./chains \
		--chain-data-dir=./chains \
		--log-dir=./logs \
		--index-enabled=true \
		--consensus-sample-size=1 \
		--consensus-quorum-size=1 \
		--api-admin-enabled=true \
		--http-allowed-origins="*"

run-testnet: build-fips init-chains
	@echo "$(GREEN)Starting Lux Testnet (ID: 96368)...$(NC)"
	@pkill -f luxd || true
	@sleep 2
	$(LUXD) \
		--network-id=96368 \
		--staking-enabled=false \
		--http-host=0.0.0.0 \
		--http-port=9630 \
		--data-dir=./chains \
		--db-dir=./chains \
		--chain-data-dir=./chains \
		--log-dir=./logs \
		--index-enabled=true

node-status:
	@echo "$(GREEN)Checking node status...$(NC)"
	@curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"info.isBootstrapped","params":{}}' \
		-H 'content-type:application/json;' http://localhost:9630/ext/info | jq

stop-node:
	@echo "$(YELLOW)Stopping Lux node...$(NC)"
	@pkill -f luxd || echo "No running node found"

# Help target
help:
	@echo "$(GREEN)Lux Node Build System$(NC)"
	@echo ""
	@echo "$(YELLOW)Configuration:$(NC)"
	@echo "  FIPS ?= 1         - Enable FIPS 140-3 mode (default: 1)"
	@echo "  CGO ?= 0          - Enable CGO (default: 0)"
	@echo "  FIPS_STRICT ?= 0  - Use FIPS strict mode (fips140=only) (default: 0)"
	@echo ""
	@echo "  Examples:"
	@echo "    make build              # Default: FIPS=1 CGO=0"
	@echo "    make build FIPS=0       # Build without FIPS"
	@echo "    make build CGO=1        # Build with CGO"
	@echo "    make test FIPS=1 CGO=1  # Test with both FIPS and CGO"
	@echo ""
	@echo "$(GREEN)Build Targets:$(NC)"
	@echo "  build         - Build luxd binary (uses FIPS=$(FIPS) CGO=$(CGO))"
	@echo "  build-fips    - Build with FIPS enabled (FIPS=1)"
	@echo "  build-cgo     - Build with CGO enabled (CGO=1)"
	@echo "  build-nocgo   - Build without CGO (CGO=0)"
	@echo "  verify-fips   - Show current FIPS/CGO configuration"
	@echo ""
	@echo "$(GREEN)Test Targets:$(NC)"
	@echo "  test          - Run all tests (uses FIPS=$(FIPS) CGO=$(CGO))"
	@echo "  test-fips     - Run tests with FIPS enabled (FIPS=1)"
	@echo "  test-cgo      - Run tests with CGO enabled (CGO=1)"
	@echo "  test-nocgo    - Run tests without CGO (CGO=0)"
	@echo "  test-short    - Run short tests only"
	@echo "  test-100      - Ensure 100% test pass rate"
	@echo "  test-unit     - Run unit tests"
	@echo "  test-integration - Run integration tests"
	@echo "  test-e2e      - Run end-to-end tests"
	@echo "  test-package  - Test specific package (use PKG=./path)"
	@echo ""
	@echo "$(GREEN)Node Operations:$(NC)"
	@echo "  run-mainnet   - Run Lux mainnet node (ID: 96369)"
	@echo "  run-testnet   - Run Lux testnet node (ID: 96368)"
	@echo "  node-status   - Check node bootstrap status"
	@echo "  stop-node     - Stop running node"
	@echo "  init-chains   - Initialize chain directories"
	@echo "  migrate-chain-data - Migrate existing chain data"
	@echo ""
	@echo "$(GREEN)Development:$(NC)"
	@echo "  fmt           - Format Go code"
	@echo "  lint          - Run linters"
	@echo "  clean         - Clean build artifacts"
	@echo "  install       - Install luxd to GOPATH/bin"
	@echo "  dev-setup     - Setup development environment"
	@echo "  list-packages - List all test packages"
	@echo "  count-packages- Count total packages"
	@echo "  help          - Show this help message"

# Build with MLX GPU acceleration support (requires CGO)
build-mlx:
	@echo "$(GREEN)Building luxd with MLX GPU acceleration (CGO enabled)...$(NC)"
	@CGO_ENABLED=1 $(ENV) ./scripts/build.sh -tags mlx
	@echo "$(GREEN)✓ Build complete with MLX support$(NC)"
