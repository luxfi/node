#!/bin/bash
set -e

# Use the existing luxd binary (ARM64) via qemu if available
if command -v qemu-aarch64-static &> /dev/null; then
    echo "Running ARM64 luxd via qemu..."
    exec qemu-aarch64-static /home/z/work/lux/node/build/luxd \
        --data-dir="/home/z/.luxd" \
        --network-id=96369 \
        --staking-enabled=false \
        --sybil-protection-enabled=false \
        --sybil-protection-disabled-weight=1000000 \
        --snow-sample-size=1 \
        --snow-quorum-size=1 \
        --http-host=0.0.0.0 \
        --http-port=9650 \
        "$@"
else
    echo "qemu-aarch64-static not found. Trying to run directly..."
    # Try running it anyway, in case binfmt_misc is configured
    exec /home/z/work/lux/node/build/luxd \
        --data-dir="/home/z/.luxd" \
        --network-id=96369 \
        --staking-enabled=false \
        --sybil-protection-enabled=false \
        --sybil-protection-disabled-weight=1000000 \
        --snow-sample-size=1 \
        --snow-quorum-size=1 \
        --http-host=0.0.0.0 \
        --http-port=9650 \
        "$@"
fi