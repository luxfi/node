#!/bin/bash
set -e

echo "🚀 Starting Lux Node"
echo "===================="

# Setup staking keys from environment or AWS KMS
if [ -n "$STAKING_KEY_KMS_ID" ]; then
    echo "📦 Fetching staking keys from AWS KMS..."
    # AWS KMS integration would go here
    # aws kms decrypt --key-id $STAKING_KEY_KMS_ID ...
elif [ -n "$STAKING_KEY" ] && [ -n "$STAKING_CERT" ]; then
    echo "🔑 Setting up staking keys from environment..."
    mkdir -p /data/staking
    echo "$STAKING_KEY" > /data/staking/staker.key
    echo "$STAKING_CERT" > /data/staking/staker.crt
    chmod 600 /data/staking/*
elif [ -d "/keys/staking" ]; then
    echo "🔑 Using mounted staking keys..."
    mkdir -p /data/staking
    cp /keys/staking/* /data/staking/ 2>/dev/null || true
    if [ -f "/data/staking/staker.crt" ] && [ -f "/data/staking/staker.key" ]; then
        chmod 600 /data/staking/*
    elif [ -f "/data/staking/staker1.crt" ] && [ -f "/data/staking/staker1.key" ]; then
        # Handle numbered staker files
        chmod 600 /data/staking/*
    fi
else
    echo "⚠️  No staking keys provided - using ephemeral certificate"
    EXTRA_ARGS="--staking-ephemeral-cert-enabled"
fi

# Setup blockchain data if provided
if [ -d "/blockchain-data" ]; then
    echo "📊 Loading blockchain data..."
    mkdir -p /data/chains/C
    if [ ! -d "/data/chains/C/chaindata" ]; then
        cp -r /blockchain-data /data/chains/C/chaindata
        echo "✅ Loaded 1,082,781 blocks"
    fi
fi

# Configure network
NETWORK_ID=${NETWORK_ID:-96369}
HTTP_HOST=${HTTP_HOST:-0.0.0.0}
HTTP_PORT=${HTTP_PORT:-9630}
STAKING_PORT=${STAKING_PORT:-9631}
LOG_LEVEL=${LOG_LEVEL:-info}

# Build command
CMD="/app/luxd"
CMD="$CMD --network-id=$NETWORK_ID"
CMD="$CMD --http-host=$HTTP_HOST"
CMD="$CMD --http-port=$HTTP_PORT"
CMD="$CMD --staking-port=$STAKING_PORT"
CMD="$CMD --log-level=$LOG_LEVEL"
CMD="$CMD --db-dir=/data/db"
CMD="$CMD --chain-data-dir=/data/chains"

# Add bootstrap nodes if provided
if [ -n "$BOOTSTRAP_IPS" ]; then
    CMD="$CMD --bootstrap-ips=$BOOTSTRAP_IPS"
fi

if [ -n "$BOOTSTRAP_IDS" ]; then
    CMD="$CMD --bootstrap-ids=$BOOTSTRAP_IDS"
fi

# Add any extra arguments
CMD="$CMD $EXTRA_ARGS $@"

echo "📝 Command: $CMD"
echo ""

# Execute
exec $CMD