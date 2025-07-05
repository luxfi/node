#!/bin/bash

# Quick launch script for testing
# This uses pre-built binaries or builds minimal components

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${YELLOW}Quick Launch - Lux Network Genesis${NC}"

# Check if avalanchego is available as fallback
if command -v avalanchego &> /dev/null; then
    echo -e "${GREEN}Found avalanchego, using it as base${NC}"
    NODE_BINARY="avalanchego"
else
    echo -e "${YELLOW}avalanchego not found, attempting minimal build${NC}"
    
    # Try to build just the main components
    echo "Building minimal node..."
    go build -tags minimal -o build/luxd-minimal ./main 2>/dev/null || {
        echo -e "${RED}Build failed. Installing avalanchego...${NC}"
        go install -v github.com/ava-labs/avalanchego/main@latest
        NODE_BINARY="$(go env GOPATH)/bin/avalanchego"
    }
fi

# Use our genesis file
GENESIS_FILE="./genesis/genesis_local_enhanced_v2.json"

if [ ! -f "$GENESIS_FILE" ]; then
    echo -e "${RED}Genesis file not found at $GENESIS_FILE${NC}"
    exit 1
fi

# Start single node for testing
echo -e "${GREEN}Starting test node with Lux genesis...${NC}"

WORK_DIR="/tmp/lux-test-node"
mkdir -p "$WORK_DIR"

# Copy genesis
cp "$GENESIS_FILE" "$WORK_DIR/genesis.json"

# Create staking keys if needed
if [ ! -f "$WORK_DIR/staker.crt" ]; then
    echo "Generating staking keys..."
    openssl req -x509 -newkey rsa:4096 -keyout "$WORK_DIR/staker.key" \
        -out "$WORK_DIR/staker.crt" -days 365 -nodes \
        -subj "/CN=lux-test-node" 2>/dev/null
fi

echo -e "${GREEN}Launching node...${NC}"

if [ -f "build/luxd-minimal" ]; then
    NODE_BINARY="./build/luxd-minimal"
fi

$NODE_BINARY \
    --network-id=local \
    --staking-enabled=false \
    --http-port=9650 \
    --db-dir="$WORK_DIR/db" \
    --log-level=info \
    --genesis="$WORK_DIR/genesis.json" \
    --staking-tls-cert-file="$WORK_DIR/staker.crt" \
    --staking-tls-key-file="$WORK_DIR/staker.key" &

NODE_PID=$!
echo $NODE_PID > "$WORK_DIR/node.pid"

echo -e "${GREEN}Node started with PID: $NODE_PID${NC}"
echo -e "${GREEN}HTTP endpoint: http://localhost:9650${NC}"
echo ""
echo -e "${YELLOW}Test commands:${NC}"
echo "  Check node ID:"
echo "    curl -X POST --data '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"info.getNodeID\"}' -H 'content-type:application/json;' http://localhost:9650/ext/info"
echo ""
echo "  Check blockchain IDs:"
echo "    curl -X POST --data '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"info.getBlockchainID\",\"params\":{\"alias\":\"X\"}}' -H 'content-type:application/json;' http://localhost:9650/ext/info"
echo ""
echo "To stop: kill $NODE_PID"