# Makefile for Lux Node

.PHONY: all build build-mlx build-release build-release-upx test clean fmt lint install-mockgen mockgen

# Configuration
CGO_ENABLED ?= 1
FIPS_STRICT ?= 0

# Go experiments come from scripts/constants.sh, which every build script also
# sources, so make and the scripts cannot compile different things.
#
# The WSL test that used to live here was a misdiagnosis: the startup SIGSEGV it
# worked around is a cgo call inside runtime/secret.Do and reproduces on any
# kernel. constants.sh carries the explanation and refuses the combination.
GOEXPERIMENT := $(shell bash -c '. ./scripts/constants.sh >/dev/null 2>&1 && echo "$$GOEXPERIMENT"')
ifeq ($(GOEXPERIMENT),)
$(error scripts/constants.sh did not yield a GOEXPERIMENT — it failed or refused the requested combination; run `bash ./scripts/constants.sh` to see why)
endif
export GOEXPERIMENT

# FIPS 140-3 always enabled (required for blockchain/financial systems)
export GOFIPS140 := latest
ifeq ($(FIPS_STRICT),1)
	export GODEBUG := fips140=only
else
	export GODEBUG := fips140=on
endif
export CGO_ENABLED

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
	@echo "FIPS_STRICT: $(FIPS_STRICT)"
	@echo "GOFIPS140: $(GOFIPS140)"
	@echo "GODEBUG: $(GODEBUG)"
	@echo "CGO_ENABLED: $${CGO_ENABLED:-not set}"
	@echo "$(GREEN)✓ Environment ready$(NC)"

# Default build
build:
	@echo "$(GREEN)Building luxd...$(NC)"
	@$(ENV) ./scripts/build.sh
	@echo "$(GREEN)✓ Build complete$(NC)"

# Default test
test:
	@echo "$(GREEN)Running tests...$(NC)"
	@$(ENV) go test -shuffle=on -race -timeout=$(TEST_TIMEOUT) -coverprofile=coverage.out -covermode=atomic $(TEST_PACKAGES)

test-short:
	@echo "Running short tests..."
	@$(ENV) go test -short -race -timeout=60s $(TEST_PACKAGES)

test-100:
	@echo "$(GREEN)=== ENSURING 100% TEST PASS RATE ===$(NC)"
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
	@echo "Running unit tests..."
	@$(ENV) go test -short -race $(TEST_PACKAGES)

test-integration:
	@echo "Running integration tests..."
	@$(ENV) go test -run Integration -race -timeout=300s $(TEST_PACKAGES)

test-e2e:
	@echo "Running e2e tests..."
	@$(ENV) ./scripts/tests.e2e.sh

# Build specific binaries
luxd:
	@echo "Building luxd..."
	@$(ENV) ./scripts/build.sh

# Installation targets
# Install to $GOPATH/bin (default go install behavior)
install:
	@echo "Installing luxd to $(GOBIN)..."
	@$(ENV) go install -v ./main
	@echo "$(GREEN)✓ Installed to $(GOBIN)/luxd$(NC)"
	@echo "Make sure $(GOBIN) is in your PATH"

# Install to /usr/local/bin (system-wide, requires sudo)
install-system: build
	@echo "Installing luxd to /usr/local/bin..."
	@sudo cp build/luxd /usr/local/bin/luxd
	@sudo chmod +x /usr/local/bin/luxd
	@echo "$(GREEN)✓ Installed to /usr/local/bin/luxd$(NC)"

# Install to ~/.local/bin (user-local, no sudo needed)
install-local: build
	@mkdir -p $(HOME)/.local/bin
	@cp build/luxd $(HOME)/.local/bin/luxd
	@chmod +x $(HOME)/.local/bin/luxd
	@echo "$(GREEN)✓ Installed to $(HOME)/.local/bin/luxd$(NC)"
	@echo "Make sure $(HOME)/.local/bin is in your PATH"

# Symlink from build dir (for development)
install-dev: build
	@echo "Creating symlink for development..."
	@ln -sf $(PWD)/build/luxd $(GOBIN)/luxd
	@echo "$(GREEN)✓ Symlinked $(PWD)/build/luxd -> $(GOBIN)/luxd$(NC)"

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
	@echo "Testing package: $(PKG)"
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
		-H 'content-type:application/json;' http://localhost:9630/v1/info | jq

stop-node:
	@echo "$(YELLOW)Stopping Lux node...$(NC)"
	@pkill -f luxd || echo "No running node found"

# Help target
help:
	@echo "$(GREEN)Lux Node Build System$(NC)"
	@echo ""
	@echo "$(YELLOW)Configuration:$(NC)"
	@echo "  CGO_ENABLED=1   - CGO enabled by default for C++/GPU backends"
	@echo "  FIPS_STRICT=0   - FIPS 140-3 always enabled, strict mode optional"
	@echo ""
	@echo "  Examples:"
	@echo "    make build                    # Build with CGO (default)"
	@echo "    CGO_ENABLED=0 make build      # Build without CGO"
	@echo ""
	@echo "$(GREEN)Build Targets:$(NC)"
	@echo "  build         - Build luxd binary"
	@echo "  build-release - Build smallest possible release binary (~46MB)"
	@echo "  build-release-upx - Build release + UPX compression (~20MB)"
	@echo "  build-mlx     - Build with MLX GPU acceleration (requires CGO)"
	@echo "  verify-fips   - Show current environment configuration"
	@echo ""
	@echo "$(GREEN)Test Targets:$(NC)"
	@echo "  test          - Run all tests (FIPS 140-3 enabled)"
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

# Release build - smallest possible binary with all optimizations
# Strips: symbols, DWARF debug info, build ID, file paths
# Disables: inlining for smaller binary, bounds check insertion
RELEASE_LDFLAGS := -s -w -buildid=
RELEASE_GCFLAGS := all=-l -B

build-release:
	@echo "$(GREEN)Building luxd release binary (optimized for size)...$(NC)"
	@mkdir -p build
	@GOWORK=off $(ENV) go build \
		-ldflags="$(RELEASE_LDFLAGS) \
			-X github.com/luxfi/node/version.GitCommit=$$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')" \
		-gcflags="$(RELEASE_GCFLAGS)" \
		-trimpath \
		-o build/luxd \
		./main
	@echo "$(GREEN)✓ Release build complete: $$(ls -lh build/luxd | awk '{print $$5}')$(NC)"

# Release build with UPX compression (if available)
build-release-upx: build-release
	@if command -v upx >/dev/null 2>&1; then \
		echo "$(GREEN)Compressing with UPX...$(NC)"; \
		upx --best -q build/luxd; \
		echo "$(GREEN)✓ Compressed: $$(ls -lh build/luxd | awk '{print $$5}')$(NC)"; \
	else \
		echo "$(YELLOW)UPX not installed. Install with: brew install upx$(NC)"; \
	fi


NODE_DOCS ?= $(HOME)/work/lux/docs/apps/docs/content/docs/nodes/reference
.PHONY: docs
docs: ## Generate the node package reference (godoc) into docs.lux.network
	cd tools/docgen && GOWORK=off go run . $(NODE_DOCS) $(CURDIR)
