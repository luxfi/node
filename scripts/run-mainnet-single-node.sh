#!/bin/bash

# Single-node mainnet runner with BLS support
# This script automates:
# 1. Generating proper staking keys with BLS
# 2. Running luxd with single-node consensus

set -e

echo "=== Lux Mainnet Single Node Runner ==="
echo "Configuring for single validator consensus..."

# Configuration
BASE_DIR="/tmp/lux-mainnet-single"
NETWORK_ID="96369"  # Mainnet
HTTP_PORT="9630"
STAKING_PORT="9631"

# Clean previous run
rm -rf "$BASE_DIR"
mkdir -p "$BASE_DIR"

# Step 1: Generate staking keys
echo ""
echo "Step 1: Generating staking keys with BLS..."
cd /home/z/work/lux/genesis
./bin/genesis staking keygen --output "$BASE_DIR/staking-keys"

# Extract node info from genesis-staker.json
NODE_ID=$(cat "$BASE_DIR/staking-keys/genesis-staker.json" | jq -r '.nodeID')
echo "Generated NodeID: $NODE_ID"

# Step 2: Create custom genesis
echo ""
echo "Step 2: Creating custom genesis configuration..."

# Extract BLS info from genesis-staker.json
STAKER_INFO=$(cat "$BASE_DIR/staking-keys/genesis-staker.json")
BLS_PUBLIC_KEY=$(echo "$STAKER_INFO" | jq -r '.signer.publicKey')
BLS_PROOF=$(echo "$STAKER_INFO" | jq -r '.signer.proofOfPossession')

# Create genesis.json with our validator
cd /home/z/work/lux/node
python3 create-single-node-genesis.py "$NODE_ID" "$BLS_PUBLIC_KEY" "$BLS_PROOF" > "$BASE_DIR/genesis.json"

echo "Created genesis.json with NodeID: $NODE_ID"

# Step 3: Create chain configurations
echo ""
echo "Step 3: Creating chain configurations..."
mkdir -p "$BASE_DIR/configs/chains/C"

# C-Chain config with BadgerDB
cat > "$BASE_DIR/configs/chains/C/config.json" <<EOF
{
  "db-type": "badgerdb",
  "log-level": "info",
  "state-sync-enabled": false,
  "offline-pruning-enabled": false,
  "allow-unprotected-txs": true
}
EOF

# Step 3: Run luxd with single-node consensus
echo ""
echo "Step 3: Starting luxd with single-node consensus..."
echo "Configuration:"
echo "  - Network ID: $NETWORK_ID (mainnet)"
echo "  - NodeID: $NODE_ID"
echo "  - HTTP Port: $HTTP_PORT"
echo "  - Staking Port: $STAKING_PORT"
echo "  - Database: BadgerDB (C-Chain), PebbleDB (P/X-Chain)"
echo "  - Consensus: Single node (k=1, alpha=1, beta=1)"
echo ""

cd /home/z/work/lux/node

# Build if needed
if [ ! -f "./build/luxd" ]; then
    echo "Building luxd..."
    make build
fi

# Set genesis database for replay
GENESIS_DB="/home/z/work/lux/genesis/state/chaindata"

# Launch with single-node consensus parameters and genesis replay
# NOTE: When using genesis-db, we don't specify genesis-file
./build/luxd \
    --network-id="$NETWORK_ID" \
    --data-dir="$BASE_DIR/data" \
    --db-type=badgerdb \
    --chain-config-dir="$BASE_DIR/configs/chains" \
    --staking-tls-cert-file="$BASE_DIR/staking-keys/staker.crt" \
    --staking-tls-key-file="$BASE_DIR/staking-keys/staker.key" \
    --staking-signer-key-file="$BASE_DIR/staking-keys/signer.key" \
    --genesis-db="$GENESIS_DB" \
    --genesis-db-type=pebbledb \
    --c-chain-db-type=badgerdb \
    --http-host=0.0.0.0 \
    --http-port="$HTTP_PORT" \
    --staking-port="$STAKING_PORT" \
    --log-level=info \
    --api-admin-enabled=true \
    --sybil-protection-enabled=true \
    --consensus-sample-size=1 \
    --consensus-quorum-size=1 \
    --consensus-commit-threshold=1 \
    --consensus-concurrent-repolls=1 \
    --consensus-optimal-processing=1 \
    --consensus-max-processing=1 \
    --consensus-max-time-processing=2s \
    --bootstrap-beacon-connection-timeout=10s \
    --health-check-frequency=2s \
    --network-max-reconnect-delay=1s