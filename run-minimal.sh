#!/bin/bash
# Ultra-minimal LUX node for C-Chain development
# Single node, no bootstrapping, immediate availability

set -e

NODE_DIR="/home/z/work/lux/node"
DATA_DIR="/home/z/work/lux/.luxd-single"

# Clean start
rm -rf "$DATA_DIR"
mkdir -p "$DATA_DIR"

# Build if needed
if [ ! -f "$NODE_DIR/build/luxd" ]; then
    echo "Building luxd..."
    cd "$NODE_DIR"
    ./scripts/build.sh
fi

echo "Starting minimal LUX node"
echo "Network ID: 96369"
echo "C-Chain RPC: http://127.0.0.1:9630/ext/bc/C/rpc"

# Run with dev mode for single-node operation
exec "$NODE_DIR/build/luxd" \
    --network-id=96369 \
    --http-host=127.0.0.1 \
    --http-port=9630 \
    --staking-port=9631 \
    --db-dir="$DATA_DIR" \
    --dev \
    --log-level=info