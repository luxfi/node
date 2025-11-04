#!/bin/bash

# C-Chain Replay Tool Build Script with EVM Integration
# (c) 2025 Lux Industries, Inc. All rights reserved.
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=========================================${NC}"
echo -e "${BLUE}   C-Chain Replay Tool Builder${NC}"
echo -e "${BLUE}   With Full EVM Transaction Replay${NC}"
echo -e "${BLUE}=========================================${NC}\n"

# Navigate to the node directory
NODE_DIR="/home/z/work/lux/node"
BIN_DIR="$NODE_DIR/bin"
cd "$NODE_DIR"

# Create bin directory if it doesn't exist
mkdir -p "$BIN_DIR"

# Check if binary exists and show its info
if [ -f "$BIN_DIR/cchain-replay" ]; then
    echo -e "${YELLOW}Existing binary found:${NC}"
    ls -lah "$BIN_DIR/cchain-replay"
    echo -e "${YELLOW}Rebuilding...${NC}\n"
fi

# Set Go environment for optimal build
export GO111MODULE=on
export CGO_ENABLED=1
export GOOS=linux
export GOARCH=amd64

# Build with optimization flags
echo -e "${GREEN}Building C-Chain Replay Tool...${NC}"
echo -e "${YELLOW}Source: vms/cchainvm/cmd/replay/main.go${NC}"
echo -e "${YELLOW}Target: $BIN_DIR/cchain-replay${NC}\n"

# Build the replay command with EVM support
go build -v \
    -ldflags="-s -w" \
    -tags="netgo osusergo" \
    -o "$BIN_DIR/cchain-replay" \
    ./vms/cchainvm/cmd/replay

if [ $? -eq 0 ]; then
    echo -e "\n${GREEN}✅ Build successful!${NC}"

    # Make executable
    chmod +x "$BIN_DIR/cchain-replay"

    # Show binary info
    echo -e "\n${GREEN}Binary Information:${NC}"
    ls -lah "$BIN_DIR/cchain-replay"

    # Test binary
    echo -e "\n${YELLOW}Testing binary...${NC}"
    if "$BIN_DIR/cchain-replay" -help 2>/dev/null | head -3 > /dev/null; then
        echo -e "${GREEN}✅ Binary test passed!${NC}"
    fi

    echo -e "\n${BLUE}Binary location:${NC} $BIN_DIR/cchain-replay"
    echo ""
    echo "Usage examples:"
    echo ""
    echo "1. Full replay to block 1,082,780 (tracking wallet):"
    echo "   ./bin/cchain-replay \\"
    echo "     -source /home/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb \\"
    echo "     -target /home/z/work/lux/node/chaindata/cchain \\"
    echo "     -height 1082780 \\"
    echo "     -wallet 0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"
    echo ""
    echo "2. Test mode (first 100 blocks only):"
    echo "   ./bin/cchain-replay \\"
    echo "     -source /home/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb \\"
    echo "     -target /tmp/test-cchain \\"
    echo "     -test -test-limit 100"
    echo ""
    echo "3. Full replay with state root verification:"
    echo "   ./bin/cchain-replay \\"
    echo "     -source /home/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb \\"
    echo "     -target /home/z/work/lux/node/chaindata/cchain \\"
    echo "     -height 1082780 \\"
    echo "     -verify"
    echo ""
    echo "Options:"
    echo "  -source       Path to source EVM database (required)"
    echo "  -target       Path to target C-Chain database (required)"
    echo "  -height       Target block height to replay to (default: 1082780)"
    echo "  -wallet       Wallet address to track balance"
    echo "  -test         Run in test mode (default: false)"
    echo "  -test-limit   Number of blocks in test mode (default: 100)"
    echo "  -verify       Verify state roots after each block"
    echo "  -log-interval Log progress every N blocks (default: 1000)"
    echo "  -chain-id     Chain ID for the network (default: 96369)"
else
    echo "❌ Build failed!"
    exit 1
fi