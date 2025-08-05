#!/bin/bash
# Launch with historic genesis replay mode
set -e

echo "=== Lux Mainnet Historic Replay Mode ==="
echo "Replaying from existing genesis data..."
echo ""

# Configuration
DATA_DIR="/tmp/lux-mainnet-replay"
NETWORK_ID="96369"
GENESIS_DB="/home/z/work/lux/genesis/state/chaindata"

# Clean directory
rm -rf "$DATA_DIR"
mkdir -p "$DATA_DIR"

# Check if luxd exists
if [ ! -f "./build/luxd" ]; then
    echo "Building luxd..."
    make build
fi

echo "Configuration:"
echo "- Network ID: $NETWORK_ID"
echo "- Data Directory: $DATA_DIR"
echo "- Genesis DB: $GENESIS_DB"
echo "- Consensus: k=1 (single node)"
echo ""

# Start with special flags to handle historic genesis
exec ./build/luxd \
    --network-id="$NETWORK_ID" \
    --data-dir="$DATA_DIR" \
    --db-type=badgerdb \
    --http-host=0.0.0.0 \
    --http-port=9630 \
    --staking-port=9631 \
    --log-level=info \
    --api-admin-enabled=true \
    --sybil-protection-enabled=false \
    --consensus-sample-size=1 \
    --consensus-quorum-size=1 \
    --consensus-commit-threshold=1 \
    --consensus-concurrent-repolls=1 \
    --consensus-optimal-processing=1 \
    --consensus-max-processing=1 \
    --consensus-max-time-processing=2s \
    --health-check-frequency=2s \
    --bootstrap-beacon-connection-timeout=10s \
    --network-max-reconnect-delay=1s \
    --consensus-app-concurrency=1