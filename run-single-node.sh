#!/bin/bash
# Minimal single-node LUX runner with C-Chain
# Network ID: 96369 (mainnet)
# Port: 9630 (RPC), 9631 (staking)

set -e

NODE_DIR="/home/z/work/lux/node"
DATA_DIR="/home/z/work/lux/.luxd-single"

# Clean start option
if [ "$1" = "clean" ]; then
    echo "Cleaning data directory..."
    rm -rf "$DATA_DIR"
fi

# Ensure data directory exists
mkdir -p "$DATA_DIR/logs"

# Build if needed
if [ ! -f "$NODE_DIR/build/luxd" ]; then
    echo "Building luxd..."
    cd "$NODE_DIR"
    ./scripts/build.sh
fi

# Run node with minimal config
echo "Starting LUX node (Network ID: 96369)"
echo "C-Chain RPC: http://127.0.0.1:9630/ext/bc/C/rpc"

exec "$NODE_DIR/build/luxd" \
    --config-file="$NODE_DIR/config-mainnet-minimal.json" \
    --genesis-file="$NODE_DIR/genesis-mainnet.json"