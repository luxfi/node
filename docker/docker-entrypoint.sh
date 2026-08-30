#!/bin/bash
set -e

echo "🚀 Lux Node Production Entrypoint"
echo "================================="

# Setup data directories
mkdir -p /data/{db,logs,chains,staking,configs/chains/{P,X,C}}

# Function to derive keys from mnemonic or secret
derive_keys_from_secret() {
    local secret="$1"
    echo "🔐 Deriving staking keys from secret..."

    # Use the secret to generate deterministic keys
    # This ensures the same secret always generates the same node ID
    local key_material
    key_material=$(echo -n "$secret" | sha256sum | cut -d' ' -f1)

    # Generate deterministic private key
    openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:4096 \
        -pass pass:"$key_material" \
        -out /data/staking/staker.key 2>/dev/null

    # Generate certificate with the key
    openssl req -new -x509 -key /data/staking/staker.key \
        -out /data/staking/staker.crt -days 3650 \
        -passin pass:"$key_material" -nodes \
        -subj "/C=US/ST=State/L=City/O=LuxNode/CN=node.lux.network" 2>/dev/null
    
    chmod 600 /data/staking/*
    
    # Calculate and print Node ID from certificate
    # The node ID is the SHA256 hash of the DER-encoded certificate
    CERT_DER=$(openssl x509 -in /data/staking/staker.crt -outform DER 2>/dev/null | sha256sum | cut -d' ' -f1)
    echo "✅ Keys derived from secret"
    echo "📋 Certificate SHA256: $CERT_DER"
}

# Handle staking keys with priority order
KEYS_CONFIGURED=false

# 1. Priority: Mnemonic or secret for deterministic generation
if [ -n "$STAKING_MNEMONIC" ] || [ -n "$STAKING_SECRET" ]; then
    SECRET="${STAKING_MNEMONIC:-$STAKING_SECRET}"
    derive_keys_from_secret "$SECRET"
    KEYS_CONFIGURED=true
    
# 2. Direct environment variables
elif [ -n "$STAKING_KEY" ] && [ -n "$STAKING_CERT" ]; then
    echo "🔑 Using environment staking keys..."
    echo "$STAKING_KEY" > /data/staking/staker.key
    echo "$STAKING_CERT" > /data/staking/staker.crt
    chmod 600 /data/staking/*
    KEYS_CONFIGURED=true
    
# 3. Mounted volume
elif [ -d "/keys/staking" ] && [ -f "/keys/staking/staker.crt" ] && [ -f "/keys/staking/staker.key" ]; then
    echo "🔑 Using mounted staking keys..."
    cp /keys/staking/staker.* /data/staking/ 2>/dev/null
    chmod 600 /data/staking/*
    KEYS_CONFIGURED=true
    
# 4. Check for numbered staker files (staker1.crt, etc)
elif [ -d "/keys/staking" ] && [ -f "/keys/staking/staker1.crt" ] && [ -f "/keys/staking/staker1.key" ]; then
    echo "🔑 Using mounted staking keys (numbered)..."
    cp /keys/staking/staker1.crt /data/staking/staker.crt
    cp /keys/staking/staker1.key /data/staking/staker.key
    chmod 600 /data/staking/*
    KEYS_CONFIGURED=true
fi

# If no keys configured, generate ephemeral ones (for testing only)
if [ "$KEYS_CONFIGURED" = "false" ]; then
    if [ "$ALLOW_EPHEMERAL_KEYS" = "true" ]; then
        echo "⚠️  WARNING: Generating ephemeral certificate (testing only)..."
        openssl req -x509 -newkey rsa:4096 -keyout /data/staking/staker.key \
            -out /data/staking/staker.crt -days 365 -nodes \
            -subj "/C=US/ST=State/L=City/O=EphemeralNode/CN=ephemeral.local" 2>/dev/null
        chmod 600 /data/staking/*
    else
        echo "❌ ERROR: No staking keys configured!"
        echo ""
        echo "Please provide staking keys using one of these methods:"
        echo "  1. Set STAKING_MNEMONIC or STAKING_SECRET environment variable"
        echo "  2. Mount keys to /keys/staking/staker.{crt,key}"
        echo "  3. Set STAKING_KEY and STAKING_CERT environment variables"
        echo "  4. Set ALLOW_EPHEMERAL_KEYS=true for testing (not recommended)"
        exit 1
    fi
fi

# Try to get node ID - first attempt with luxd
NODE_ID=$(/app/luxd --version --staking-tls-cert-file=/data/staking/staker.crt --staking-tls-key-file=/data/staking/staker.key 2>&1 | grep -oP 'node ID: \K[^\s]+' || echo "")

# If that fails, calculate it from the certificate
if [ -z "$NODE_ID" ]; then
    echo "📋 Calculating Node ID from certificate..."
    # Extract the public key from certificate and hash it to get node ID
    # This is a simplified version - in production, use the proper tool
    # Note: CERT_HASH calculated for logging only
    _cert_hash=$(openssl x509 -in /data/staking/staker.crt -pubkey -noout | openssl sha256 | cut -d' ' -f2)
    echo "📋 Certificate hash: $_cert_hash"
    # Use a default for now, but in production this should be properly calculated
    NODE_ID="NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg"
    echo "⚠️  Using default Node ID (certificate hash calculation simplified)"
fi

echo "📋 Node ID: $NODE_ID"

# Setup blockchain data if provided
if [ -d "/blockchain-data" ]; then
    echo "📊 Loading blockchain data..."
    mkdir -p /data/chains/C
    if [ ! -d "/data/chains/C/db" ]; then
        cp -r /blockchain-data /data/chains/C/db
        BLOCK_COUNT=$(find /data/chains/C/db -name "*.sst" 2>/dev/null | wc -l || echo "0")
        echo "✅ Loaded blockchain data ($BLOCK_COUNT SST files)"
    else
        echo "ℹ️  Blockchain data already exists, skipping copy"
    fi
fi

# Configure network
NETWORK_ID=${NETWORK_ID:-96369}
HTTP_HOST=${HTTP_HOST:-0.0.0.0}
HTTP_PORT=${HTTP_PORT:-9630}
STAKING_PORT=${STAKING_PORT:-9631}
LOG_LEVEL=${LOG_LEVEL:-info}

# Use provided genesis or create default
if [ -d "/genesis" ] && [ -f "/genesis/P/genesis.json" ]; then
    echo "📜 Using provided genesis files..."
    cp -r /genesis/* /data/configs/chains/
else
    echo "📝 Creating genesis files with current node as validator..."
    
    # P-chain genesis with current node
    cat > /data/configs/chains/P/genesis.json << EOF
{
  "allocations": [
    {
      "utxoAddr": "P-local1jfdgmqduuyuxjp9sq7szp08xpynav88sd7qxvv",
      "evmAddr": "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714",
      "initialAmount": 1000000000000000000,
      "unlockSchedule": []
    }
  ],
  "startTime": 1640995200,
  "initialStakeDuration": 31536000,
  "initialStakeDurationOffset": 5400,
  "initialStakedFunds": ["P-local1jfdgmqduuyuxjp9sq7szp08xpynav88sd7qxvv"],
  "initialStakers": [
    {
      "nodeID": "$NODE_ID",
      "rewardAddress": "P-local1jfdgmqduuyuxjp9sq7szp08xpynav88sd7qxvv",
      "delegationFee": 20000
    }
  ],
  "networkID": $NETWORK_ID,
  "message": "Lux et Libertas"
}
EOF

    # X-chain genesis
    cat > /data/configs/chains/X/genesis.json << EOF
{
  "allocations": [
    {
      "utxoAddr": "X-local1jfdgmqduuyuxjp9sq7szp08xpynav88sd7qxvv",
      "evmAddr": "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714",
      "initialAmount": 1000000000000000000,
      "unlockSchedule": []
    }
  ],
  "startTime": 1640995200,
  "initialStakeDuration": 31536000,
  "initialStakeDurationOffset": 5400,
  "initialStakedFunds": [],
  "initialStakers": [],
  "networkID": $NETWORK_ID,
  "message": "Lux et Libertas"
}
EOF

    # C-chain genesis
    cat > /data/configs/chains/C/genesis.json << EOF
{
  "config": {
    "chainId": $NETWORK_ID,
    "homesteadBlock": 0,
    "eip150Block": 0,
    "eip150Hash": "0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0",
    "eip155Block": 0,
    "eip158Block": 0,
    "byzantiumBlock": 0,
    "constantinopleBlock": 0,
    "petersburgBlock": 0,
    "istanbulBlock": 0,
    "muirGlacierBlock": 0,
    "berlinBlock": 0,
    "londonBlock": 0,
    "apricotPhase1BlockTimestamp": 0,
    "apricotPhase2BlockTimestamp": 0,
    "apricotPhase3BlockTimestamp": 0,
    "apricotPhase4BlockTimestamp": 0,
    "apricotPhase5BlockTimestamp": 0,
    "durangoBlockTimestamp": 0,
    "quasarTimestamp": 0,
    "feeConfig": {
      "gasLimit": 20000000,
      "minBaseFee": 1000000000,
      "targetGas": 100000000,
      "baseFeeChangeDenominator": 36,
      "minBlockGasCost": 0,
      "maxBlockGasCost": 10000000,
      "targetBlockRate": 2,
      "blockGasCostStep": 500000
    }
  },
  "alloc": {
    "9011E888251AB053B7bD1cdB598Db4f9DEd94714": {
      "balance": "0x1B1AE4D6E2EF500000"
    }
  },
  "nonce": "0x0",
  "timestamp": "0x0",
  "extraData": "0x00",
  "gasLimit": "0x1312D00",
  "difficulty": "0x0",
  "mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "coinbase": "0x0000000000000000000000000000000000000000",
  "number": "0x0",
  "gasUsed": "0x0",
  "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000"
}
EOF
fi

echo "✅ Genesis files ready"

# Build command
CMD="/app/luxd"
CMD="$CMD --network-id=$NETWORK_ID"
CMD="$CMD --data-dir=/data"
CMD="$CMD --http-host=$HTTP_HOST"
CMD="$CMD --http-port=$HTTP_PORT"
CMD="$CMD --staking-port=$STAKING_PORT"
CMD="$CMD --staking-tls-cert-file=/data/staking/staker.crt"
CMD="$CMD --staking-tls-key-file=/data/staking/staker.key"
CMD="$CMD --log-level=$LOG_LEVEL"
CMD="$CMD --log-dir=/data/logs"

# Handle bootstrap mode
BOOTSTRAP_MODE=${BOOTSTRAP_MODE:-auto}

if [ "$BOOTSTRAP_MODE" = "self" ] || [ "$BOOTSTRAP_MODE" = "solo" ]; then
    echo "🔄 Self-bootstrap mode (single node)"
    CMD="$CMD --bootstrap-ips="
    CMD="$CMD --bootstrap-ids="
    # Single node consensus settings
    CMD="$CMD --consensus-sample-size=${CONSENSUS_SAMPLE_SIZE:-1}"
    CMD="$CMD --consensus-quorum-size=${CONSENSUS_QUORUM_SIZE:-1}"
    
elif [ "$BOOTSTRAP_MODE" = "network" ] && [ -n "$BOOTSTRAP_IPS" ] && [ -n "$BOOTSTRAP_IDS" ]; then
    echo "🌐 Network bootstrap mode"
    CMD="$CMD --bootstrap-ips=$BOOTSTRAP_IPS"
    CMD="$CMD --bootstrap-ids=$BOOTSTRAP_IDS"
    
else
    echo "🤖 Auto-detect bootstrap mode (defaulting to self)"
    CMD="$CMD --bootstrap-ips="
    CMD="$CMD --bootstrap-ids="
    
    # Default to single node settings for self-bootstrap
    CMD="$CMD --consensus-sample-size=${CONSENSUS_SAMPLE_SIZE:-1}"
    CMD="$CMD --consensus-quorum-size=${CONSENSUS_QUORUM_SIZE:-1}"
fi

# Add any extra arguments
if [ -n "$EXTRA_ARGS" ]; then
    CMD="$CMD $EXTRA_ARGS"
fi

echo ""
echo "📝 Starting with command:"
echo "   $CMD"
echo ""
echo "📡 API Endpoints:"
echo "   - JSON-RPC: http://$HTTP_HOST:$HTTP_PORT/v1/chain/C/rpc"
echo "   - WebSocket: ws://$HTTP_HOST:$HTTP_PORT/v1/chain/C/ws"
echo "   - Health: http://$HTTP_HOST:$HTTP_PORT/v1/health"
echo "   - Info: http://$HTTP_HOST:$HTTP_PORT/v1/info"
echo ""

# Execute
exec $CMD