#!/bin/bash
# Network Manager - Boot mainnet/testnet/local with regenesis support

set -e

NETWORK_TYPE=${1:-local}

case $NETWORK_TYPE in
  mainnet)
    NETWORK_ID=96369
    DATA_DIR=~/.lux/mainnet
    GENESIS_SOURCE=~/work/lux/state/chaindata/lux-mainnet-96369/bootnodes.json
    REGENESIS_SOURCE=~/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb
    ;;
  testnet)
    NETWORK_ID=96368
    DATA_DIR=~/.lux/testnet
    GENESIS_SOURCE=~/work/lux/state/chaindata/lux-testnet-96368/bootnodes.json
    REGENESIS_SOURCE=~/work/lux/state/chaindata/lux-testnet-96368/db/pebbledb
    ;;
  local)
    NETWORK_ID=12345
    DATA_DIR=~/.lux/local
    GENESIS_SOURCE=""
    REGENESIS_SOURCE=""
    ;;
  *)
    echo "Usage: $0 {mainnet|testnet|local}"
    exit 1
    ;;
esac

echo "═══════════════════════════════════════════"
echo "  NETWORK: $NETWORK_TYPE (ID: $NETWORK_ID)"
echo "═══════════════════════════════════════════"
echo ""

# Clean old network
rm -rf "$DATA_DIR"
mkdir -p "$DATA_DIR"

# Run regenesis if source exists
if [ -n "$REGENESIS_SOURCE" ] && [ -d "$REGENESIS_SOURCE" ]; then
    echo "🌌 Running regenesis from historic chain..."
    cd ~/work/lux/node
    ./build/regenesis "$REGENESIS_SOURCE" "$DATA_DIR/db/C"

    # Initialize metadata
    echo "📝 Initializing blockchain metadata..."
    go run ./cmd/init-from-snapshot
    echo ""
fi

# Start 5-node network
echo "🚀 Starting 5-node $NETWORK_TYPE network..."
echo ""

cd ~/work/lux/netrunner
go run ./examples/local/fivenodenetwork/main.go

echo ""
echo "✅ Network running!"
echo "   Query balance: curl -X POST --data '{\"jsonrpc\":\"2.0\",\"method\":\"eth_getBalance\",\"params\":[\"0x9011E888251AB053B7bD1cdB598Db4f9DEd94714\",\"latest\"],\"id\":1}' http://localhost:9650/ext/bc/C/rpc"
