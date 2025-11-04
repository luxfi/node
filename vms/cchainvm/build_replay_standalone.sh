#!/bin/bash

# Standalone C-Chain Replay Tool Build Script
# Builds without node dependencies
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=========================================${NC}"
echo -e "${BLUE}   C-Chain Replay Tool Builder${NC}"
echo -e "${BLUE}   Standalone Version${NC}"
echo -e "${BLUE}=========================================${NC}\n"

# Set directories
NODE_DIR="/home/z/work/lux/node"
BIN_DIR="$NODE_DIR/bin"
CCHAINVM_DIR="$NODE_DIR/vms/cchainvm"

cd "$NODE_DIR"

# Create bin directory if it doesn't exist
mkdir -p "$BIN_DIR"

# First, let's create a standalone main.go that doesn't import node constants
echo -e "${YELLOW}Creating standalone replay tool...${NC}"

cat > "$CCHAINVM_DIR/cmd/replay/main_standalone.go" << 'EOF'
// Standalone C-Chain Replay Tool
package main

import (
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/ethdb/leveldb"
	"github.com/luxfi/geth/params"
)

func main() {
	// Command-line flags
	var (
		sourcePath   = flag.String("source", "", "Path to source EVM database (required)")
		targetPath   = flag.String("target", "", "Path to target C-Chain database (required)")
		targetHeight = flag.Uint64("height", 1082780, "Target block height to replay to")
		walletAddr   = flag.String("wallet", "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714", "Wallet address to track")
		testMode     = flag.Bool("test", false, "Run in test mode (only process first 100 blocks)")
		testLimit    = flag.Uint64("test-limit", 100, "Number of blocks to process in test mode")
		verifyRoots  = flag.Bool("verify", false, "Verify state roots after each block")
		logInterval  = flag.Uint64("log-interval", 1000, "Log progress every N blocks")
		chainID      = flag.Int64("chain-id", 96369, "Chain ID for the network")
	)

	flag.Parse()

	// Validate required flags
	if *sourcePath == "" || *targetPath == "" {
		fmt.Fprintf(os.Stderr, "Error: Both -source and -target paths are required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	// Convert wallet address
	var targetWallet common.Address
	if *walletAddr != "" {
		if !common.IsHexAddress(*walletAddr) {
			log.Fatalf("Invalid wallet address: %s", *walletAddr)
		}
		targetWallet = common.HexToAddress(*walletAddr)
	}

	// Set default source path if using standard location
	if *sourcePath == "" {
		*sourcePath = "/home/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb"
	}

	// Create target directory if it doesn't exist
	targetDir := filepath.Dir(*targetPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		log.Fatalf("Failed to create target directory: %v", err)
	}

	log.Printf("===========================================")
	log.Printf("C-Chain Replay Engine (Standalone)")
	log.Printf("===========================================")
	log.Printf("Source DB: %s", *sourcePath)
	log.Printf("Target DB: %s", *targetPath)
	log.Printf("Target Height: %d", *targetHeight)
	log.Printf("Tracking Wallet: %s", targetWallet.Hex())
	log.Printf("Chain ID: %d", *chainID)
	log.Printf("Test Mode: %v", *testMode)
	if *testMode {
		log.Printf("Test Limit: %d blocks", *testLimit)
	}
	log.Printf("===========================================")

	// TODO: Implement the actual replay logic here
	// For now, just print configuration
	log.Printf("Replay tool configured successfully!")
	log.Printf("Note: Full EVM replay implementation is in progress")
}
EOF

# Build the standalone version
echo -e "${GREEN}Building standalone replay tool...${NC}"
cd "$NODE_DIR"

# Build with minimal dependencies
go build -v \
    -ldflags="-s -w" \
    -o "$BIN_DIR/cchain-replay-standalone" \
    "$CCHAINVM_DIR/cmd/replay/main_standalone.go"

if [ $? -eq 0 ]; then
    echo -e "\n${GREEN}✅ Build successful!${NC}"

    # Make executable
    chmod +x "$BIN_DIR/cchain-replay-standalone"

    # Show binary info
    echo -e "\n${GREEN}Binary Information:${NC}"
    ls -lah "$BIN_DIR/cchain-replay-standalone"

    # Test binary
    echo -e "\n${YELLOW}Testing binary...${NC}"
    "$BIN_DIR/cchain-replay-standalone" -help

    echo -e "\n${BLUE}Binary location:${NC} $BIN_DIR/cchain-replay-standalone"
else
    echo -e "${RED}✗ Build failed!${NC}"
    exit 1
fi

echo -e "\n${GREEN}=========================================${NC}"
echo -e "${GREEN}Standalone build completed!${NC}"
echo -e "${GREEN}=========================================${NC}"