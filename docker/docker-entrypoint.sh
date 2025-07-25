#!/bin/bash
set -e

# Generate staking certificates if they don't exist
if [ ! -f "/luxd/staking/staker.crt" ] || [ ! -f "/luxd/staking/staker.key" ]; then
    echo "Generating staking certificates..."
    mkdir -p /luxd/staking
    cd /luxd/staking
    openssl genrsa -out staker.key 4096 2>/dev/null
    openssl req -new -x509 -key staker.key -out staker.crt -days 365 -subj "/C=US/ST=NY/O=Lux/CN=lux" 2>/dev/null
    cd /luxd
    echo "Certificates generated successfully"
fi

# Check if we need to prepare for chain data import
if [ -n "$PREPARE_IMPORT" ] && [ "$PREPARE_IMPORT" = "true" ]; then
    echo "Preparing for chain data import after network startup..."
    
    # Create a marker file to indicate import is pending
    if [ ! -f "/luxd/.import_pending" ]; then
        touch /luxd/.import_pending
        echo "Import will be initiated after network is running"
    fi
fi

# Set up C-Chain configuration
echo "Creating C-Chain configuration..."
mkdir -p /luxd/configs/chains/C

cat > /luxd/configs/chains/C/config.json << EOF
{
  "linear-api-enabled": false,
  "coreth-admin-api-enabled": true,
  "eth-apis": ["eth", "eth-filter", "net", "web3", "internal-eth", "internal-blockchain", "internal-debug", "internal-tx-pool", "debug", "trace"],
  "personal-api-enabled": true,
  "tx-pool-api-enabled": true,
  "debug-api-enabled": true,
  "trace-api-enabled": true,
  "net-api-enabled": true,
  "web3-api-enabled": true,
  "eth-api-enabled": true,
  "internal-public-api-enabled": true,
  "database-backend": "pebbledb",
  "local-txs-enabled": true,
  "api-max-duration": 0,
  "api-max-blocks-per-request": 0,
  "api-max-gas-per-request": 0,
  "ws-enabled": true,
  "ws-port": 8546,
  "allow-unfinalized-queries": true,
  "log-level": "${LOG_LEVEL:-info}",
  "state-sync-enabled": false,
  "pruning-enabled": false,
  "enable-automining": ${ENABLE_AUTOMINING:-true},
  "automining-interval": "${AUTOMINING_INTERVAL:-2s}",
  "tx-pool-price-limit": 1,
  "tx-pool-account-slots": 16,
  "tx-pool-global-slots": 4096,
  "tx-pool-account-queue": 64,
  "tx-pool-global-queue": 1024,
  "rpc-gas-cap": 0,
  "rpc-tx-fee-cap": 0
}
EOF

# Set up genesis if not exists
if [ ! -f "/luxd/configs/chains/C/genesis.json" ] && [ -n "$GENESIS_FILE" ]; then
    echo "Copying genesis file..."
    cp "$GENESIS_FILE" /luxd/configs/chains/C/genesis.json
elif [ ! -f "/luxd/configs/chains/C/genesis.json" ]; then
    echo "Creating default genesis..."
    cat > /luxd/configs/chains/C/genesis.json << 'EOF'
{
  "config": {
    "chainId": 96369,
    "homesteadBlock": 0,
    "eip150Block": 0,
    "eip155Block": 0,
    "eip158Block": 0,
    "byzantiumBlock": 0,
    "constantinopleBlock": 0,
    "petersburgBlock": 0,
    "istanbulBlock": 0,
    "muirGlacierBlock": 0,
    "berlinBlock": 0,
    "londonBlock": 0,
    "feeConfig": {
      "gasLimit": 12000000,
      "targetBlockRate": 2,
      "minBaseFee": 25000000000,
      "targetGas": 60000000,
      "baseFeeChangeDenominator": 36,
      "minBlockGasCost": 0,
      "maxBlockGasCost": 1000000,
      "blockGasCostStep": 200000
    },
    "warpConfig": {
      "blockTimestamp": 1750805381,
      "quorumNumerator": 67
    }
  },
  "nonce": "0x0",
  "timestamp": "0x685b2b85",
  "extraData": "0x",
  "gasLimit": "0xb71b00",
  "difficulty": "0x0",
  "mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "coinbase": "0x0000000000000000000000000000000000000000",
  "alloc": {
    "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714": {
      "balance": "0x193e5939a08ce9dbd480000000"
    }
  },
  "airdropHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "airdropAmount": null,
  "number": "0x0",
  "gasUsed": "0x0",
  "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "baseFeePerGas": null,
  "excessBlobGas": null,
  "blobGasUsed": null
}
EOF
fi

# Handle initial data setup for validators
if [ -n "$NODE_ID" ] && [ -d "/import/c-chain" ] && [ -d "/import/p-chain" ]; then
    echo "Setting up validator node $NODE_ID..."
    
    # Setup database directories
    mkdir -p "/luxd/db/luxnet/${NETWORK_ID:-96369}/chains"
    
    # Copy C-Chain data if not already present
    C_CHAIN_DIR="/luxd/db/luxnet/${NETWORK_ID:-96369}/chains/C"
    if [ ! -d "$C_CHAIN_DIR" ] || [ -z "$(ls -A $C_CHAIN_DIR 2>/dev/null)" ]; then
        echo "Copying C-Chain data..."
        mkdir -p "$C_CHAIN_DIR"
        cp -r /import/c-chain/* "$C_CHAIN_DIR/"
    fi
    
    # Copy P-Chain data if not already present
    P_CHAIN_DIR="/luxd/db/luxnet/${NETWORK_ID:-96369}/chains/11111111111111111111111111111111LpoYY"
    if [ ! -d "$P_CHAIN_DIR" ] || [ -z "$(ls -A $P_CHAIN_DIR 2>/dev/null)" ]; then
        echo "Copying P-Chain data..."
        mkdir -p "$P_CHAIN_DIR"
        cp -r /import/p-chain/* "$P_CHAIN_DIR/"
    fi
fi

# Start the node
echo "Starting Lux node..."

# Build command arguments
CMD_ARGS=(
    "--data-dir=/luxd"
    "--network-id=${NETWORK_ID:-96369}"
    "--http-host=0.0.0.0"
    "--http-port=${HTTP_PORT:-9650}"
    "--staking-port=${STAKING_PORT:-9651}"
    "--chain-config-dir=/luxd/configs/chains"
    "--index-allow-incomplete"
    "--force-ignore-checksum"
    "--log-level=${LOG_LEVEL:-info}"
)

# Add bootstrap nodes if provided
if [ -n "$BOOTSTRAP_IPS" ]; then
    CMD_ARGS+=("--bootstrap-ips=$BOOTSTRAP_IPS")
fi

if [ -n "$BOOTSTRAP_IDS" ]; then
    CMD_ARGS+=("--bootstrap-ids=$BOOTSTRAP_IDS")
fi

# Add conditional arguments
if [ "${DEV_MODE:-false}" = "true" ]; then
    CMD_ARGS+=("--dev")
fi

if [ "${API_ADMIN_ENABLED:-true}" = "true" ]; then
    CMD_ARGS+=("--api-admin-enabled")
fi

if [ "${INDEX_ENABLED:-true}" = "true" ]; then
    CMD_ARGS+=("--index-enabled")
fi

# For mainnet validators, add specific settings
if [ -n "$NODE_ID" ]; then
    CMD_ARGS+=(
        "--staking-enabled=true"
        "--sybil-protection-enabled=true"
        "--consensus-sample-size=5"
        "--consensus-quorum-size=3"
        "--consensus-virtuous-commit-threshold=5"
        "--consensus-rogue-commit-threshold=10"
    )
else
    # Single node or dev mode
    CMD_ARGS+=(
        "--staking-enabled=false"
        "--sybil-protection-enabled=false"
        "--consensus-sample-size=1"
        "--consensus-quorum-size=1"
    )
fi

exec luxd "${CMD_ARGS[@]}" "$@"