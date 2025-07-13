#!/bin/bash

# Run Lux Node (luxd) with proper configuration

# Set up directories
DATA_DIR="${LUX_DATA_DIR:-$HOME/.luxd}"
PLUGIN_DIR="${DATA_DIR}/plugins"

# Create directories if they don't exist
mkdir -p "${DATA_DIR}"
mkdir -p "${PLUGIN_DIR}"

# Copy EVM plugin if it exists
if [ -f "./build/plugins/evm" ]; then
    echo "Copying EVM plugin..."
    cp ./build/plugins/evm "${PLUGIN_DIR}/"
fi

# Set default parameters
NETWORK_ID="${NETWORK_ID:-local}"
HTTP_PORT="${HTTP_PORT:-9650}"
STAKING_PORT="${STAKING_PORT:-9651}"

echo "Starting Lux Node (luxd)..."
echo "Network: ${NETWORK_ID}"
echo "Data Directory: ${DATA_DIR}"
echo "HTTP Port: ${HTTP_PORT}"
echo "Staking Port: ${STAKING_PORT}"

# Run luxd
./build/luxd \
    --network-id="${NETWORK_ID}" \
    --data-dir="${DATA_DIR}" \
    --http-host=0.0.0.0 \
    --http-port="${HTTP_PORT}" \
    --staking-port="${STAKING_PORT}" \
    --plugin-dir="${PLUGIN_DIR}" \
    --log-level=info \
    "$@"