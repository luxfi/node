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

# Check if we need to perform data migration
if [ -n "$IMPORT_CHAIN_DATA" ] && [ -d "$IMPORT_CHAIN_DATA" ]; then
    echo "Chain data import path specified: $IMPORT_CHAIN_DATA"
    
    # Check if import has already been done
    if [ ! -f "/luxd/.import_done" ]; then
        echo "Preparing chain data import..."
        
        # We'll handle the import differently since it's subnet-evm data
        # For now, we'll skip the import and let the node start fresh
        # In production, you'd want to write a custom migration tool
        
        echo "Note: Direct import from subnet-evm to C-Chain requires custom migration"
        echo "Starting with fresh chain data instead"
        
        touch /luxd/.import_done
    else
        echo "Import already completed, skipping..."
    fi
fi

# Set up C-Chain configuration if not exists
if [ ! -f "/luxd/configs/chains/C/config.json" ]; then
    echo "Creating C-Chain configuration..."
    cat > /luxd/configs/chains/C/config.json << EOF
{
  "snowman-api-enabled": false,
  "coreth-admin-api-enabled": true,
  "eth-apis": ["eth", "eth-filter", "net", "web3", "internal-eth", "internal-blockchain", "internal-debug", "debug", "personal", "admin", "miner", "txpool"],
  "personal-api-enabled": true,
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
fi

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

# Start the node
echo "Starting Lux node..."

# Build command arguments
CMD_ARGS=(
    "--data-dir=/luxd"
    "--network-id=${NETWORK_ID:-96369}"
    "--http-host=0.0.0.0"
    "--http-port=9650"
    "--chain-config-dir=/luxd/configs/chains"
    "--index-allow-incomplete"
    "--force-ignore-checksum"
    "--log-level=${LOG_LEVEL:-info}"
)

# Add conditional arguments
if [ "${DEV_MODE:-true}" = "true" ]; then
    CMD_ARGS+=("--dev")
fi

if [ "${API_ADMIN_ENABLED:-true}" = "true" ]; then
    CMD_ARGS+=("--api-admin-enabled")
fi

if [ "${INDEX_ENABLED:-true}" = "true" ]; then
    CMD_ARGS+=("--index-enabled")
fi

exec luxd "${CMD_ARGS[@]}" "$@"