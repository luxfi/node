#!/bin/bash

# LUX Full Mainnet with all 3 chains
# P-Chain, X-Chain, and C-Chain

echo "Starting LUX Full Mainnet with all chains..."

# Ensure build is available
if [ ! -f "./build/luxd" ]; then
    echo "Building luxd binary..."
    ./scripts/build.sh
fi

# Kill any existing process
pkill -f luxd || true
sleep 2

# Set environment for C-Chain
export LUXD_DATA_DIR="$HOME/.luxd"
export CORETH_CHAIN_ID=96369

# Start with full configuration
./build/luxd \
  --network-id=96369 \
  --http-host=0.0.0.0 \
  --http-port=9630 \
  --staking-port=9631 \
  --db-dir="$LUXD_DATA_DIR/db" \
  --chain-data-dir="$LUXD_DATA_DIR/chainData" \
  --chain-config-dir="$LUXD_DATA_DIR/configs/chains" \
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
  --api-admin-enabled=true \
  --api-auth-required=false \
  --api-ipcs-enabled=true \
  --api-keystore-enabled=true \
  --api-metrics-enabled=true \
  --index-enabled=true \
  --index-allow-incomplete=true \
  --http-allowed-origins="*" \
  --http-allowed-hosts="*" \
  --coreth-config-file="$LUXD_DATA_DIR/configs/chains/C/config.json" \
  "$@"