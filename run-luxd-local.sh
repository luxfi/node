#!/bin/bash
set -e

# Use existing .luxd data or fall back to .avalanchego
DATA_DIR="${DATA_DIR:-$HOME/.luxd}"
if [ ! -d "$DATA_DIR" ] && [ -d "$HOME/.avalanchego" ]; then
    echo "Using .avalanchego data directory"
    DATA_DIR="$HOME/.avalanchego"
fi

# Create data directory if it doesn't exist
mkdir -p "$DATA_DIR"

echo "Starting luxd with data directory: $DATA_DIR"
echo "Network ID: 96369 (LUX Mainnet)"

# Run luxd with compatible avalanchego flags
exec /home/z/work/lux/node/build/luxd-x86_64 \
    --data-dir="$DATA_DIR" \
    --network-id=96369 \
    --http-host=0.0.0.0 \
    --http-port=9650 \
    --public-ip-resolution-service=opendns \
    --index-enabled \
    --index-allow-incomplete \
    "$@"