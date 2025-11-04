#!/bin/bash

# C-Chain Replay Execution Script
# Performs full EVM-based transaction replay from EVM to C-Chain
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Configuration
REPLAY_BINARY="/home/z/work/lux/node/bin/cchain-replay"
SOURCE_DB="/home/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb"
TARGET_DB="/home/z/work/lux/node/chaindata/cchain"
TARGET_HEIGHT=1082780
WALLET_ADDRESS="0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"
CHAIN_ID=96369

# Parse command line arguments
MODE="full"
while [[ $# -gt 0 ]]; do
    case $1 in
        --test)
            MODE="test"
            shift
            ;;
        --verify)
            MODE="verify"
            shift
            ;;
        --clean)
            MODE="clean"
            shift
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --test    Run in test mode (100 blocks only)"
            echo "  --verify  Run with state root verification"
            echo "  --clean   Clean target database before replay"
            echo "  --help    Show this help message"
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

echo -e "${CYAN}=========================================${NC}"
echo -e "${CYAN}   C-Chain EVM Replay Engine${NC}"
echo -e "${CYAN}=========================================${NC}\n"

# Check if binary exists
if [ ! -f "$REPLAY_BINARY" ]; then
    echo -e "${RED}Error: Replay binary not found at $REPLAY_BINARY${NC}"
    echo -e "${YELLOW}Building replay tool...${NC}"
    cd /home/z/work/lux/node/vms/cchainvm
    ./build_replay.sh
    if [ ! -f "$REPLAY_BINARY" ]; then
        echo -e "${RED}Build failed. Exiting.${NC}"
        exit 1
    fi
fi

# Check if source database exists
if [ ! -d "$SOURCE_DB" ]; then
    echo -e "${RED}Error: Source database not found at $SOURCE_DB${NC}"
    exit 1
fi

# Clean target database if requested
if [ "$MODE" = "clean" ] || [ ! -d "$TARGET_DB" ]; then
    echo -e "${YELLOW}Cleaning target database...${NC}"
    rm -rf "$TARGET_DB"
    mkdir -p "$TARGET_DB"
    echo -e "${GREEN}✓ Target database cleaned${NC}\n"
    MODE="full"  # Continue with full replay after cleaning
fi

# Show configuration
echo -e "${BLUE}Configuration:${NC}"
echo -e "  Mode: ${YELLOW}$MODE${NC}"
echo -e "  Source DB: ${YELLOW}$SOURCE_DB${NC}"
echo -e "  Target DB: ${YELLOW}$TARGET_DB${NC}"
echo -e "  Target Height: ${YELLOW}$TARGET_HEIGHT${NC}"
echo -e "  Wallet: ${YELLOW}$WALLET_ADDRESS${NC}"
echo -e "  Chain ID: ${YELLOW}$CHAIN_ID${NC}"
echo -e "${CYAN}=========================================${NC}\n"

# Function to run replay
run_replay() {
    local args=""

    # Base arguments
    args="$args -source $SOURCE_DB"
    args="$args -target $TARGET_DB"
    args="$args -height $TARGET_HEIGHT"
    args="$args -wallet $WALLET_ADDRESS"
    args="$args -chain-id $CHAIN_ID"

    # Mode-specific arguments
    case $1 in
        test)
            args="$args -test -test-limit 100"
            echo -e "${YELLOW}Running in TEST mode (100 blocks)...${NC}"
            ;;
        verify)
            args="$args -verify"
            echo -e "${YELLOW}Running with state root verification...${NC}"
            ;;
        *)
            echo -e "${YELLOW}Running full replay...${NC}"
            ;;
    esac

    # Execute replay
    echo -e "${GREEN}Starting EVM replay...${NC}\n"
    $REPLAY_BINARY $args
}

# Record start time
START_TIME=$(date +%s)

# Run the replay
run_replay $MODE

# Record end time
END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

# Format duration
HOURS=$((DURATION / 3600))
MINUTES=$(((DURATION % 3600) / 60))
SECONDS=$((DURATION % 60))

echo -e "\n${CYAN}=========================================${NC}"
echo -e "${GREEN}✅ Replay completed successfully!${NC}"
echo -e "${BLUE}Duration: ${HOURS}h ${MINUTES}m ${SECONDS}s${NC}"
echo -e "${BLUE}Target database: $TARGET_DB${NC}"
echo -e "${CYAN}=========================================${NC}\n"

# Show database size
if [ -d "$TARGET_DB" ]; then
    SIZE=$(du -sh "$TARGET_DB" | cut -f1)
    echo -e "${BLUE}Database size: ${YELLOW}$SIZE${NC}"
fi

echo -e "\n${GREEN}Next steps:${NC}"
echo -e "1. Verify the database: ${YELLOW}ls -la $TARGET_DB${NC}"
echo -e "2. Check wallet balance in the replayed state"
echo -e "3. Start C-Chain with the new database"