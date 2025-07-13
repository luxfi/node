#!/bin/bash

# Run Lux Node locally with minimal configuration

cd "$(dirname "$0")"

# Create a minimal test environment
TEST_DIR="/tmp/luxd-local-$$"
mkdir -p "$TEST_DIR/plugins"

# Copy plugin if exists
if [ -f "./build/plugins/evm" ]; then
    cp ./build/plugins/evm "$TEST_DIR/plugins/"
fi

echo "Starting Lux Node in local test mode..."
echo "Data directory: $TEST_DIR"

# Try to run the luxd binary with minimal config
exec ./build/luxd \
    --network-id=local \
    --data-dir="$TEST_DIR" \
    --http-host=127.0.0.1 \
    --http-port=9650 \
    --staking-enabled=false \
    --log-level=info