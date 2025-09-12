#!/bin/bash

# LUX Mainnet Node Startup Script
# Network ID: 96369
# Port: 9630
# Mode: 1/1 Validator (NOT POA)

# Set data directory
export LUXD_DATA_DIR="${LUXD_DATA_DIR:-$HOME/.luxd}"

# Ensure build is available
if [ ! -f "./build/luxd" ]; then
    echo "Building luxd binary..."
    ./scripts/build.sh
fi

# Kill any existing luxd process
pkill -f luxd || true

echo "Starting LUX Mainnet Node..."
echo "Network ID: 96369"
echo "HTTP Port: 9630"
echo "Staking Port: 9631"
echo "Data Directory: $LUXD_DATA_DIR"
echo "Mode: 1/1 Validator Consensus"
echo ""

# Start the node with proper configuration
./build/luxd \
  --network-id=96369 \
  --http-host=0.0.0.0 \
  --http-port=9630 \
  --staking-port=9631 \
  --db-dir="$LUXD_DATA_DIR/db" \
  --chain-data-dir="$LUXD_DATA_DIR/chainData" \
  --log-dir="$LUXD_DATA_DIR/logs" \
  --log-level=info \
  --snow-sample-size=1 \
  --snow-quorum-size=1 \
  --snow-virtuous-commit-threshold=1 \
  --snow-rogue-commit-threshold=1 \
  --snow-concurrent-repolls=1 \
  --snow-optimal-processing=1 \
  --minimum-stake-amount=1000000000000000 \
  --staking-enabled=true \
  --sybil-protection-enabled=false \
  --genesis-file=/home/z/work/lux/genesis/configs/lux-mainnet-96369/genesis.json \
  --chain-config-dir=/home/z/work/lux/genesis/configs/lux-mainnet-96369 \
  --api-admin-enabled=true \
  --api-auth-required=false \
  --api-ipcs-enabled=true \
  --api-keystore-enabled=true \
  --api-metrics-enabled=true \
  --http-allowed-origins="*" \
  --http-allowed-hosts="*" \
  "$@"