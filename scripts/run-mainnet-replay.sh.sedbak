#!/bin/bash
set -euo pipefail

# Script to run luxd with BadgerDB and replay subnet EVM blocks using genesis-db

# Configuration
NETWORK_ID=96369
DATA_DIR="$HOME/.luxd-mainnet-badger"
STAKING_DIR="$DATA_DIR/staking"
NODE_COUNT=1  # Use 1 node for single-node consensus, or 5 for multi-node
GENESIS_DB_PATH="/home/z/work/lux/genesis/state/chaindata/C-import/pebbledb"
GENESIS_DB_TYPE="pebbledb"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== LUX Mainnet Replay Setup ===${NC}"
echo "Network ID: $NETWORK_ID"
echo "Data Directory: $DATA_DIR"
echo "Genesis DB: $GENESIS_DB_PATH"
echo "Database Backend: BadgerDB"
echo ""

# Kill any existing luxd process
echo -e "${YELLOW}Stopping any existing luxd process...${NC}"
pkill -f luxd || true
sleep 2

# Clean and create directories
echo -e "${YELLOW}Setting up directories...${NC}"
rm -rf "$DATA_DIR"
mkdir -p "$DATA_DIR"
mkdir -p "$DATA_DIR/configs/chains/C"
mkdir -p "$DATA_DIR/logs"

# Generate staking keys
echo -e "${YELLOW}Generating staking keys...${NC}"
cd /home/z/work/lux/genesis
./bin/genesis staking keygen --output "$STAKING_DIR"

# Get NodeID from generated keys
NODE_ID=$(./bin/genesis staking info --cert "$STAKING_DIR/staker.crt" | grep "Node ID:" | awk '{print $3}')
echo -e "${GREEN}Generated Node ID: $NODE_ID${NC}"

# Create C-Chain config for POA mode
echo -e "${YELLOW}Creating C-Chain configuration...${NC}"
cat > "$DATA_DIR/configs/chains/C/config.json" << EOF
{
  "snowman-api-enabled": false,
  "coreth-admin-api-enabled": false,
  "pruning-enabled": false,
  "local-txs-enabled": true,
  "api-max-duration": 0,
  "api-max-blocks-per-request": 0,
  "api-max-headers-per-request": 0,
  "api-max-block-headers-per-request": 0,
  "api-max-rlp-batch-size": 0,
  "state-sync-enabled": false,
  "allow-unfinalized-queries": true,
  "skip-upgrade-check": true,
  "skip-upgrade-height": 0,
  "api-require-auth": false,
  "consensus-timeout": 5000000000,
  "eth-apis": ["eth", "personal", "admin", "debug", "web3", "internal-debug", "internal-blockchain", "internal-transaction-pool", "net"],
  "continuous-profiler-dir": "",
  "continuous-profiler-frequency": 900000000000,
  "continuous-profiler-max-files": 5,
  "allow-unprotected-txs": true,
  "allow-unprotected-tx-hashes": [],
  "preimages-enabled": false,
  "offline-pruning-enabled": false,
  "offline-pruning-data-directory": "",
  "max-outbound-active-requests": 16,
  "max-outbound-active-cross-chain-requests": 64,
  "remote-tx-gossip-only-enabled": false,
  "log-level": "info",
  "log-json-format": false
}
EOF

# Run luxd with proper parameters
echo -e "${GREEN}Starting LUX node with BadgerDB and genesis replay...${NC}"
echo "Using genesis database at: $GENESIS_DB_PATH"
echo ""

cd /home/z/work/lux/node

# Build command with parameters
LUXD_CMD="./build/luxd \
  --network-id=$NETWORK_ID \
  --db-type=badgerdb \
  --genesis-db=$GENESIS_DB_PATH \
  --genesis-db-type=$GENESIS_DB_TYPE \
  --data-dir=$DATA_DIR \
  --chain-config-dir=$DATA_DIR/configs/chains \
  --staking-tls-cert-file=$STAKING_DIR/staker.crt \
  --staking-tls-key-file=$STAKING_DIR/staker.key \
  --staking-signer-key-file=$STAKING_DIR/signer.key \
  --http-host=0.0.0.0 \
  --http-port=9630 \
  --staking-port=9631 \
  --log-level=info \
  --log-dir=$DATA_DIR/logs \
  --sybil-protection-enabled=false \
  --api-admin-enabled=true \
  --index-enabled=false"

if [ "$NODE_COUNT" -eq 1 ]; then
    # Single node mode - use consensus parameters for single validator
    echo -e "${YELLOW}Running in single-node mode${NC}"
    LUXD_CMD="$LUXD_CMD \
      --consensus-sample-size=1 \
      --consensus-quorum-size=1"
else
    # Multi-node mode - use standard mainnet consensus parameters
    echo -e "${YELLOW}Running in multi-node mode ($NODE_COUNT nodes)${NC}"
    echo -e "${RED}Note: You'll need to generate keys for other nodes and configure bootstrap peers${NC}"
fi

echo -e "${GREEN}Launching luxd...${NC}"
echo "Command: $LUXD_CMD"
echo ""

# Run luxd
exec $LUXD_CMD 2>&1 | tee "$DATA_DIR/logs/luxd.log"