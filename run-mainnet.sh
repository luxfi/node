#!/bin/bash

# Kill any existing luxd process
pkill luxd 2>/dev/null
sleep 2

# Set up data directory
DATA_DIR=~/.luxd-mainnet
GENESIS_FILE=/home/z/work/lux/genesis/configs/mainnet/genesis.json

echo "Starting Lux Mainnet with single validator (POA mode)..."
echo "Network ID: 96369"
echo "Data Directory: $DATA_DIR"
echo ""

# Launch luxd with POA configuration for single validator
./build/luxd \
  --network-id=96369 \
  --data-dir=$DATA_DIR \
  --genesis-file=$GENESIS_FILE \
  --http-host=0.0.0.0 \
  --public-ip=127.0.0.1 \
  --log-level=info \
  --api-admin-enabled \
  --api-keystore-enabled \
  --api-metrics-enabled \
  --index-enabled \
  --chain-config-dir=$DATA_DIR/configs/chains

echo ""
echo "Node started! Checking status..."
sleep 5

# Check if node is running
if pgrep luxd > /dev/null; then
    echo "✅ Node is running"
    echo ""
    echo "Checking network info..."
    curl -s -X POST --data '{"jsonrpc":"2.0","method":"info.getNetworkID","params":{},"id":1}' \
         -H 'content-type:application/json;' http://127.0.0.1:9630/ext/info | python3 -m json.tool
    
    echo ""
    echo "Checking node ID..."
    curl -s -X POST --data '{"jsonrpc":"2.0","method":"info.getNodeID","params":{},"id":1}' \
         -H 'content-type:application/json;' http://127.0.0.1:9630/ext/info | python3 -m json.tool
else
    echo "❌ Node failed to start. Check logs:"
    tail -20 nohup.out
fi