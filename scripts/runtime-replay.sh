#!/bin/bash

# Lux Runtime Regenesis Replay Script
# Replays SubnetEVM blocks (1,074,616) to C-Chain

set -e

echo "╔═══════════════════════════════════════════════════════════════╗"
echo "║          LUX RUNTIME REGENESIS REPLAY TOOL                   ║"
echo "╚═══════════════════════════════════════════════════════════════╝"
echo ""

# Configuration
REPLAY_DIR="$HOME/.lux-runtime-replay"
SOURCE_DB_PATH="$HOME/work/lux/state/chaindata/lux-mainnet-96369/db"  # Old mainnet database
TARGET_DB_PATH="$REPLAY_DIR/cchain-db"  # New C-Chain database
NUM_BLOCKS=1074616
TREASURY_ADDR="0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"
TREASURY_AMOUNT="61500000000"  # 61.5 billion LUX
CCHAIN_REPLAY_TOOL="/Users/z/work/lux/node/cchain-replay"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

# Step 1: Check prerequisites
echo -e "${YELLOW}📋 Checking prerequisites...${NC}"

if ! command -v lux-cli &> /dev/null; then
    echo -e "${RED}❌ lux-cli not found. Please install it first.${NC}"
    echo "   Run: go install github.com/luxfi/cli/cmd/lux@latest"
    exit 1
fi

if ! command -v docker &> /dev/null; then
    echo -e "${RED}❌ Docker not found. Please install Docker first.${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Prerequisites satisfied${NC}"
echo ""

# Step 2: Create replay directory
echo -e "${YELLOW}📁 Setting up replay directory...${NC}"
rm -rf "$REPLAY_DIR"
mkdir -p "$REPLAY_DIR"
cd "$REPLAY_DIR"

# Step 3: Deploy local network
echo -e "${YELLOW}🚀 Deploying multi-node network...${NC}"
echo "   This will start a 5-node local network"
lux network start --num-nodes 5 --avalanchego-version latest

# Wait for network to be ready
echo -e "${YELLOW}⏳ Waiting for network to initialize...${NC}"
sleep 10

# Step 4: Check source database
echo -e "${YELLOW}⚙️  Checking source database (old mainnet 96369)...${NC}"
echo "   Database path: $SOURCE_DB_PATH"
echo "   Total blocks to replay: $NUM_BLOCKS"

if [ ! -d "$SOURCE_DB_PATH" ]; then
    echo -e "${RED}❌ Source database not found at: $SOURCE_DB_PATH${NC}"
    echo "   Please ensure the old mainnet database is available"
    exit 1
fi
echo -e "${GREEN}✅ Source database found${NC}"

# Create subnet configuration
cat > subnet-config.json << EOF
{
  "chainId": 96369,
  "homesteadBlock": 0,
  "eip150Block": 0,
  "eip150Hash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "eip155Block": 0,
  "eip158Block": 0,
  "byzantiumBlock": 0,
  "constantinopleBlock": 0,
  "petersburgBlock": 0,
  "istanbulBlock": 0,
  "muirGlacierBlock": 0,
  "subnetEVMTimestamp": 0,
  "feeConfig": {
    "gasLimit": 8000000,
    "targetBlockRate": 2,
    "minBaseFee": 25000000000,
    "targetGas": 15000000,
    "baseFeeChangeDenominator": 36,
    "minBlockGasCost": 0,
    "maxBlockGasCost": 1000000,
    "blockGasCostStep": 200000
  }
}
EOF

echo -e "${GREEN}✅ SubnetEVM configured${NC}"

# Step 5: Configure C-Chain for replay
echo -e "${YELLOW}⚙️  Configuring C-Chain for runtime regenesis replay...${NC}"

# Create C-Chain genesis with treasury allocation
cat > cchain-genesis.json << EOF
{
  "config": {
    "chainId": 96369,
    "homesteadBlock": 0,
    "eip150Block": 0,
    "eip150Hash": "0x0000000000000000000000000000000000000000000000000000000000000000",
    "eip155Block": 0,
    "eip158Block": 0,
    "byzantiumBlock": 0,
    "constantinopleBlock": 0,
    "petersburgBlock": 0,
    "istanbulBlock": 0,
    "muirGlacierBlock": 0
  },
  "nonce": "0x0",
  "timestamp": "0x0",
  "extraData": "0x00",
  "gasLimit": "0x5f5e100",
  "difficulty": "0x0",
  "mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "coinbase": "0x0000000000000000000000000000000000000000",
  "alloc": {
    "$TREASURY_ADDR": {
      "balance": "0x33b2e3c9fd0803ce8000000"
    }
  }
}
EOF

echo -e "${GREEN}✅ C-Chain configured with treasury allocation${NC}"

# Step 6: Build cchain-replay tool if needed
if [ ! -f "$CCHAIN_REPLAY_TOOL" ]; then
    echo -e "${YELLOW}🔨 Building cchain-replay tool...${NC}"
    cd /Users/z/work/lux/node
    go build -o cchain-replay ./cmd/cchain-replay
    if [ $? -ne 0 ]; then
        echo -e "${RED}❌ Failed to build cchain-replay tool${NC}"
        exit 1
    fi
fi

# Step 7: Start runtime replay
echo -e "${YELLOW}🔄 Starting runtime replay from old mainnet (96369) to C-Chain...${NC}"
echo "   Source: $SOURCE_DB_PATH"
echo "   Target: $TARGET_DB_PATH"
echo "   Blocks to replay: $NUM_BLOCKS"
echo "   Treasury: $TREASURY_AMOUNT LUX"
echo ""
echo -e "${YELLOW}⏳ This will take some time. Replaying $NUM_BLOCKS blocks...${NC}"

# Run the actual replay
$CCHAIN_REPLAY_TOOL \
    -source "$SOURCE_DB_PATH" \
    -target "$TARGET_DB_PATH" \
    -backend auto \
    -type auto \
    -height "$NUM_BLOCKS" \
    -wallet "$TREASURY_ADDR" \
    -verify

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✅ Replay completed successfully!${NC}"
else
    echo -e "${RED}❌ Replay failed. Check logs for details.${NC}"
    exit 1
fi

# Step 7: Verify results
echo ""
echo -e "${YELLOW}✅ Verification Steps:${NC}"
echo "   1. Check C-Chain block height:"
echo "      curl -X POST --data '{\"jsonrpc\":\"2.0\",\"method\":\"eth_blockNumber\",\"params\":[],"id":1}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc"
echo ""
echo "   2. Check treasury balance:"
echo "      curl -X POST --data '{\"jsonrpc\":\"2.0\",\"method\":\"eth_getBalance\",\"params\":[\"$TREASURY_ADDR\", \"latest\"],\"id\":1}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc"
echo ""

echo -e "${GREEN}✅ Runtime replay setup complete!${NC}"
echo ""
echo "📁 Output directory: $REPLAY_DIR"
echo "   • subnet-config.json - SubnetEVM configuration"
echo "   • cchain-genesis.json - C-Chain genesis with treasury"
echo ""
echo -e "${YELLOW}🎯 Expected Results:${NC}"
echo "   • C-Chain shows 1,074,616 blocks after replay"
echo "   • Treasury balance: ~61.5 billion LUX"
echo ""
echo -e "${GREEN}Runtime replay ready to execute!${NC}"