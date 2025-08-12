#!/bin/bash
# Run luxd with genesis database replay for historic blocks

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NODE_DIR="$(dirname "$SCRIPT_DIR")"

# Configuration
NETWORK_ID=96369
DATA_DIR="$HOME/.luxd-mainnet-replay"
GENESIS_DB="/home/z/work/lux/genesis/state/chaindata/lux-mainnet-96369/db"
HTTP_PORT=9630
STAKING_PORT=9631

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}Starting luxd with genesis database replay${NC}"
echo "Network ID: $NETWORK_ID"
echo "Genesis DB: $GENESIS_DB"
echo "Data Directory: $DATA_DIR"

# Check if genesis database exists
if [ ! -d "$GENESIS_DB" ]; then
    echo -e "${RED}Error: Genesis database not found at $GENESIS_DB${NC}"
    echo "Please ensure the migrated database is available"
    exit 1
fi

# Create data directory if it doesn't exist
mkdir -p "$DATA_DIR"

# Check if luxd binary exists
if [ ! -f "$NODE_DIR/build/luxd" ]; then
    echo -e "${RED}Error: luxd binary not found${NC}"
    echo "Please build the node first with: make build"
    exit 1
fi

# Export environment variables for replay
export LUX_GENESIS=1
export LUX_GENESIS_IMPORT_PATH="$GENESIS_DB"

echo -e "${YELLOW}Starting luxd with block replay enabled...${NC}"
echo "This will replay 1,082,781 historic blocks from the SubnetEVM"
echo ""

# Run luxd with genesis database
exec "$NODE_DIR/build/luxd" \
    --network-id=$NETWORK_ID \
    --genesis-db="$GENESIS_DB" \
    --genesis-db-type=pebbledb \
    --db-type=badgerdb \
    --p-chain-db-type=badgerdb \
    --x-chain-db-type=badgerdb \
    --c-chain-db-type=badgerdb \
    --data-dir="$DATA_DIR" \
    --http-host=0.0.0.0 \
    --http-port=$HTTP_PORT \
    --staking-port=$STAKING_PORT \
    --staking-tls-cert-file=/tmp/staker.crt \
    --staking-tls-key-file=/tmp/staker.key \
    --staking-signer-key-file=/tmp/signer.key \
    --chain-data-dir="$DATA_DIR/chainData" \
    --log-level=info