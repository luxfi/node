# Lux Node Makefile

# Variables
BINARY_NAME := luxd
BUILD_DIR := build
LINUX_BINARY := $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64
MAC_BINARY := $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64
DOCKER_IMAGE := ghcr.io/luxfi/node
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Go build flags
GO_BUILD_FLAGS := -tags "pebbledb debug vmdebug"
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: all build build-linux build-mac docker clean help tidy

# Default target
all: build

# Help target
help:
	@echo "Lux Node Build System"
	@echo ""
	@echo "Targets:"
	@echo "  make build          - Build for current platform"
	@echo "  make build-linux    - Build Linux AMD64 binary"
	@echo "  make build-mac      - Build macOS ARM64 binary"
	@echo "  make docker         - Build Docker image"
	@echo "  make docker-push    - Push Docker image to registry"
	@echo "  make clean          - Clean build artifacts"
	@echo "  make test           - Run tests"
	@echo "  make run            - Run local node"
	@echo ""
	@echo "Docker targets:"
	@echo "  make docker-run     - Run node in Docker"
	@echo "  make docker-stop    - Stop Docker container"
	@echo "  make docker-logs    - View Docker logs"

# Build for current platform
build:
	@echo "🔨 Building $(BINARY_NAME) for current platform..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 go build $(GO_BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./main

# Build Linux binary
build-linux:
	@echo "🐧 Building $(BINARY_NAME) for Linux AMD64..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) $(LDFLAGS) -o $(LINUX_BINARY) ./main
	@echo "✅ Linux binary: $(LINUX_BINARY)"

# Build macOS binary
build-mac:
	@echo "🍎 Building $(BINARY_NAME) for macOS ARM64..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build $(GO_BUILD_FLAGS) $(LDFLAGS) -o $(MAC_BINARY) ./main
	@echo "✅ macOS binary: $(MAC_BINARY)"

tidy:
	go mod tidy

# Build Docker image
docker: build-linux
	@echo "🐳 Building Docker image $(DOCKER_IMAGE):$(VERSION)..."
	@cp $(LINUX_BINARY) docker/luxd
	@docker build -t $(DOCKER_IMAGE):$(VERSION) -t $(DOCKER_IMAGE):latest docker/
	@rm docker/luxd
	@echo "✅ Docker image built: $(DOCKER_IMAGE):$(VERSION)"

# Push Docker image
docker-push:
	@echo "📤 Pushing Docker image $(DOCKER_IMAGE):$(VERSION)..."
	docker push $(DOCKER_IMAGE):$(VERSION)
	docker push $(DOCKER_IMAGE):latest

# Run local node
run: build
	@echo "🚀 Starting Lux node..."
	./$(BUILD_DIR)/$(BINARY_NAME) \
		--network-id=96369 \
		--data-dir=~/.luxd \
		--http-host=0.0.0.0 \
		--staking-enabled=false \
		--api-admin-enabled=true \
		--index-enabled=true

# Run node in Docker
docker-run:
	@echo "🐳 Running Lux node in Docker..."
	@docker run -d \
		--name luxd-node \
		--network lux-network \
		-p 9630:9630 \
		-p 9631:9631 \
		-v ~/.luxd:/luxd \
		$(DOCKER_IMAGE):latest

# Stop Docker container
docker-stop:
	@echo "🛑 Stopping Docker container..."
	@docker stop luxd-node || true
	@docker rm luxd-node || true

# View Docker logs
docker-logs:
	@docker logs -f luxd-node

# Run tests
test:
	@echo "🧪 Running tests..."
	go test -tags "pebbledb debug vmdebug test" ./...

# Clean build artifacts
clean:
	@echo "🧹 Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@echo "✅ Clean complete"

# Install binary
install: build
	@echo "📦 Installing $(BINARY_NAME) to /usr/local/bin..."
	@sudo cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/
	@echo "✅ Installation complete"
