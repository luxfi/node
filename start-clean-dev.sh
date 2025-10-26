#!/bin/bash

# Clean start script for Lux development mode
# Runs on port 9630 with chain ID 1337

set -e

echo "🧹 Cleaning up any existing processes..."
killall -9 luxd 2>/dev/null || true
sleep 2

echo "🚀 Starting Lux node in development mode..."
echo "   Network ID: 1337"
echo "   Chain ID: 1337"
echo "   HTTP Port: 9630"
echo "   Staking Port: 9631"

DATA_DIR="/tmp/luxd-clean-dev-$$"
rm -rf "$DATA_DIR"

cd "$(dirname "$0")"

./build/luxd \
  --network-id=1337 \
  --http-port=9630 \
  --staking-port=9631 \
  --data-dir="$DATA_DIR" \
  --log-level=info \
  --index-enabled \
  --api-admin-enabled \
  --api-ipcs-enabled \
  --api-keystore-enabled \
  --api-metrics-enabled \
  --http-host=0.0.0.0 \
  --chain-config-dir=./configs/chains