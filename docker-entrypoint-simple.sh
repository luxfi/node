#!/bin/bash
set -e

# Default data directory
DATA_DIR="${DATA_DIR:-/data}"
NETWORK_ID="${NETWORK_ID:-96369}"

echo "Starting luxd with network ID: $NETWORK_ID"
echo "Data directory: $DATA_DIR"

# Run luxd with basic configuration
exec /usr/local/bin/luxd \
    --data-dir="$DATA_DIR" \
    --network-id="$NETWORK_ID" \
    --http-host=0.0.0.0 \
    --http-port=9650 \
    --http-allowed-hosts="*" \
    --http-allowed-origins="*" \
    --public-ip-resolution-service=opendns \
    --api-admin-enabled=true \
    --index-enabled=true \
    --index-allow-incomplete=true \
    --log-level="${LOG_LEVEL:-info}" \
    "$@"