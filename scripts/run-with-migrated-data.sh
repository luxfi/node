#!/bin/bash

# Launch Lux node with migrated C-Chain data
# Uses the already migrated blockchain data at ~/.luxd/chainData/C/db/badgerdb/ethdb

set -e

echo "=== Lux Mainnet with Migrated Data ==="
echo "Using migrated blockchain data with 1,082,780 blocks"

# Configuration
DATA_DIR="/home/z/.luxd"
NETWORK_ID="96369"  # Mainnet
HTTP_PORT="9630"
STAKING_PORT="9631"

# Use existing staking keys
STAKING_DIR="$DATA_DIR/staking"
if [ ! -f "$STAKING_DIR/staker.crt" ]; then
    echo "ERROR: Staking keys not found at $STAKING_DIR"
    echo "Please generate staking keys first"
    exit 1
fi

# Extract NodeID from certificate
NODE_ID=$(openssl x509 -in "$STAKING_DIR/staker.crt" -noout -text | grep "Subject:" | sed 's/.*CN = //' | cut -d',' -f1)
echo "Using existing staking keys"

# Ensure chain config exists
mkdir -p "$DATA_DIR/configs/chains/C"
cat > "$DATA_DIR/configs/chains/C/config.json" <<EOF
{
  "db-type": "badgerdb",
  "log-level": "info",
  "state-sync-enabled": false,
  "offline-pruning-enabled": false,
  "allow-unprotected-txs": true,
  "continuous-profiler-dir": "",
  "continuous-profiler-frequency": 0,
  "continuous-profiler-max-files": 0,
  "api-max-duration": 0,
  "ws-cpu-refill-rate": 0,
  "ws-cpu-max-stored": 0,
  "api-max-blocks-per-request": 0,
  "allow-unfinalized-queries": false,
  "accepted-cache-size": 32
}
EOF

echo ""
echo "Configuration:"
echo "  - Network ID: $NETWORK_ID (mainnet)"
echo "  - NodeID: $NODE_ID"
echo "  - HTTP Port: $HTTP_PORT"
echo "  - Staking Port: $STAKING_PORT"
echo "  - Data Directory: $DATA_DIR"
echo "  - C-Chain Database: $DATA_DIR/chainData/C/db/badgerdb/ethdb"
echo "  - Migrated blocks: 1,082,780"
echo "  - Consensus: Single node (k=1)"
echo ""

cd /home/z/work/lux/node

# Build if needed
if [ ! -f "./build/luxd" ]; then
    echo "Building luxd..."
    make build
fi

# Launch with migrated data
# The node will use the existing database at ~/.luxd/chainData/C/db/badgerdb/ethdb
./build/luxd \
    --network-id="$NETWORK_ID" \
    --data-dir="$DATA_DIR" \
    --db-type=pebbledb \
    --chain-config-dir="$DATA_DIR/configs/chains" \
    --staking-tls-cert-file="$STAKING_DIR/staker.crt" \
    --staking-tls-key-file="$STAKING_DIR/staker.key" \
    --staking-signer-key-file="$STAKING_DIR/signer.key" \
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