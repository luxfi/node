#!/bin/bash

# Direct replay script using the C-Chain VM's runtime replay capability

set -e

echo "╔═══════════════════════════════════════════════════════════════╗"
echo "║          LUX DIRECT RUNTIME REPLAY (96369 → C-Chain)         ║"
echo "╚═══════════════════════════════════════════════════════════════╝"
echo ""

# Configuration
SOURCE_DB="$HOME/work/lux/state/chaindata/lux-mainnet-96369/db"
TARGET_DIR="$HOME/.lux-cchain-replay"
EXPECTED_BLOCKS=1074616
TREASURY_ADDR="0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m'

echo -e "${YELLOW}📋 Configuration:${NC}"
echo "   • Source: Old mainnet (96369) at $SOURCE_DB"
echo "   • Target: New C-Chain at $TARGET_DIR"
echo "   • Expected blocks: $EXPECTED_BLOCKS"
echo "   • Treasury: $TREASURY_ADDR"
echo ""

# Step 1: Check source database
echo -e "${YELLOW}🔍 Checking source database...${NC}"
if [ ! -d "$SOURCE_DB" ]; then
    echo -e "${RED}❌ Source database not found at: $SOURCE_DB${NC}"
    exit 1
fi

# Check what type of database it is
if [ -d "$SOURCE_DB/badgerdb" ]; then
    echo -e "${GREEN}✅ Found BadgerDB database${NC}"
    DB_TYPE="badger"
elif [ -d "$SOURCE_DB/pebbledb" ]; then
    echo -e "${GREEN}✅ Found PebbleDB database${NC}"
    DB_TYPE="pebble"
elif [ -d "$SOURCE_DB/leveldb" ]; then
    echo -e "${GREEN}✅ Found LevelDB database${NC}"
    DB_TYPE="level"
else
    # Try to detect by files
    if [ -f "$SOURCE_DB/MANIFEST" ]; then
        echo -e "${GREEN}✅ Found BadgerDB database (by MANIFEST)${NC}"
        DB_TYPE="badger"
    else
        echo -e "${YELLOW}⚠️  Unknown database type, will try auto-detection${NC}"
        DB_TYPE="auto"
    fi
fi

# Step 2: Create target directory
echo -e "${YELLOW}📁 Setting up target directory...${NC}"
rm -rf "$TARGET_DIR"
mkdir -p "$TARGET_DIR"

# Step 3: Set environment variable for runtime replay
export CHAIN_DATA_DIR="$HOME/work/lux/state/chaindata/lux-mainnet-96369"
export LUX_RUNTIME_REPLAY="true"
export LUX_REPLAY_HEIGHT="$EXPECTED_BLOCKS"

echo -e "${YELLOW}🚀 Starting replay process...${NC}"
echo "   This will replay $EXPECTED_BLOCKS blocks from the old mainnet"
echo "   Database type: $DB_TYPE"
echo ""

# Option 1: Try using the node directly with replay enabled
echo -e "${YELLOW}Option 1: Using luxd node with runtime replay${NC}"
cat > "$TARGET_DIR/replay-config.json" << EOF
{
  "network-id": 1,
  "db-dir": "$TARGET_DIR/db",
  "log-level": "info",
  "http-host": "127.0.0.1",
  "http-port": 9650,
  "runtime-replay": {
    "enabled": true,
    "source-path": "$SOURCE_DB",
    "source-type": "$DB_TYPE",
    "target-height": $EXPECTED_BLOCKS,
    "verify": true
  }
}
EOF

echo -e "${GREEN}✅ Replay configuration created${NC}"
echo ""
echo -e "${YELLOW}📝 To run the replay:${NC}"
echo ""
echo "1. Start luxd with runtime replay:"
echo "   cd /Users/z/work/lux/node"
echo "   ./build/node --config-file=$TARGET_DIR/replay-config.json"
echo ""
echo "2. Or set environment and run:"
echo "   export CHAIN_DATA_DIR=\"$CHAIN_DATA_DIR\""
echo "   export LUX_RUNTIME_REPLAY=\"true\""
echo "   export LUX_REPLAY_HEIGHT=\"$EXPECTED_BLOCKS\""
echo "   ./build/node"
echo ""
echo "3. After replay completes, verify:"
echo "   # Check block height"
echo "   curl -X POST --data '{\"jsonrpc\":\"2.0\",\"method\":\"eth_blockNumber\",\"params\":[],\"id\":1}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc"
echo ""
echo "   # Check treasury balance"
echo "   curl -X POST --data '{\"jsonrpc\":\"2.0\",\"method\":\"eth_getBalance\",\"params\":[\"$TREASURY_ADDR\", \"latest\"],\"id\":1}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc"
echo ""
echo -e "${YELLOW}Expected results:${NC}"
echo "   • Block height: 0x106518 (hex for $EXPECTED_BLOCKS)"
echo "   • Treasury balance: 61.5 billion LUX"
echo ""
echo -e "${GREEN}Ready to start replay!${NC}"