#!/bin/bash
# Start node with historic lux-mainnet-96369 state

set -e

echo "═══════════════════════════════════════════"
echo "  STARTING NODE WITH HISTORIC STATE"
echo "═══════════════════════════════════════════"
echo ""

# Ensure migrated data exists
if [ ! -d ~/.lux/db/C ]; then
    echo "❌ No migrated database found at ~/.lux/db/C"
    echo "Run regenesis tool first!"
    exit 1
fi

echo "✅ Found migrated database ($(du -sh ~/.lux/db/C | cut -f1))"
echo ""

# Check for genesis from historic chain
GENESIS_SOURCE=~/work/lux/state/chaindata/lux-mainnet-96369/bootnodes.json
if [ ! -f "$GENESIS_SOURCE" ]; then
    echo "❌ Historic genesis not found"
    exit 1
fi

echo "✅ Historic genesis: $GENESIS_SOURCE"
echo ""

# The key insight: Use the EXACT same database the node creates
# Copy our migrated data INTO the node's database namespace

# Clean start
rm -rf ~/.lux/db-mainnet

# Create proper structure
mkdir -p ~/.lux/db-mainnet
cp -r ~/.lux/db/C ~/.lux/db-mainnet/

echo "🚀 Starting luxd with historic state..."
echo ""

cd "$(dirname "$0")/.."

# Start node pointing to our database
./build/luxd \
  --network-id=96369 \
  --http-port=9650 \
  --staking-port=9651 \
  --log-level=info \
  --data-dir=~/.lux/db-mainnet \
  --public-ip=127.0.0.1

echo "Node started!"
