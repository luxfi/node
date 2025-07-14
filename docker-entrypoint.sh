#!/bin/bash
set -e

# Default data directory
DATA_DIR="${DATA_DIR:-/data}"

# Network configuration
NETWORK_ID="${NETWORK_ID:-96369}"
CHAIN_ID="${CHAIN_ID:-96369}"

# POA mode configuration
POA_MODE="${POA_MODE:-true}"
AUTOMINING="${AUTOMINING:-true}"
MIN_BLOCK_TIME="${MIN_BLOCK_TIME:-1s}"

# Build luxd command with flags
LUXD_ARGS=(
    "--data-dir=${DATA_DIR}"
    "--network-id=${NETWORK_ID}"
    "--http-host=0.0.0.0"
    "--http-port=9650"
    "--http-allowed-hosts=*"
    "--http-allowed-origins=*"
    "--public-ip=${PUBLIC_IP:-127.0.0.1}"
    "--health-check-frequency=2s"
    "--api-admin-enabled=true"
    "--api-auth-required=false"
    "--api-ipcs-enabled=true"
    "--index-enabled=true"
    "--log-level=${LOG_LEVEL:-info}"
)

# Add POA configuration if enabled
if [ "$POA_MODE" = "true" ]; then
    echo "Starting in POA mode with automining=${AUTOMINING}"
    LUXD_ARGS+=(
        "--staking-enabled=false"
        "--sybil-protection-enabled=false"
        "--sybil-protection-disabled-weight=1000000"
        "--snow-sample-size=1"
        "--snow-quorum-size=1"
        "--snow-concurrent-repolls=1"
        "--snow-optimal-processing=1"
    )
    
    # Configure coreth for POA mode
    CORETH_CONFIG='{
        "pruning-enabled": false,
        "eth-apis": ["eth", "eth-filter", "net", "web3", "admin", "debug", "personal", "txpool", "miner"],
        "local-txs-enabled": true,
        "api-max-duration": 0,
        "api-max-blocks-per-request": 0,
        "allow-unfinalized-queries": true,
        "tx-pool-price-limit": 1'
    
    # Add automining configuration if enabled
    if [ "$AUTOMINING" = "true" ]; then
        CORETH_CONFIG+=',
        "continuous-profiler-enabled": false,
        "miner": {
            "enabled": true,
            "threads": 1,
            "notify-full": false,
            "gas-price": "0x0",
            "gas-limit": "0x7A1200",
            "etherbase": "'${ETHERBASE:-0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC}'",
            "extra-data": "0x00"
        },
        "block-production": {
            "enabled": true,
            "mode": "continuous",
            "min-block-time": "'${MIN_BLOCK_TIME}'"
        }'
    fi
    
    CORETH_CONFIG+='}'
    LUXD_ARGS+=("--coreth-config=$CORETH_CONFIG")
fi

# Add any additional arguments passed to the script
if [ $# -gt 0 ]; then
    # If first argument is 'luxd', skip it
    if [ "$1" = "luxd" ]; then
        shift
    fi
    LUXD_ARGS+=("$@")
fi

# Execute luxd with all arguments
echo "Starting luxd with arguments: ${LUXD_ARGS[@]}"
exec /usr/local/bin/luxd "${LUXD_ARGS[@]}"