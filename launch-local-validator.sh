#!/bin/bash

# Standard Lux data directory
DATA_DIR=~/.luxd

# Kill any existing luxd process
pkill luxd 2>/dev/null
sleep 2

# Create necessary directories
mkdir -p $DATA_DIR/configs/chains/C

# Copy C-Chain config to standard location
cp /home/z/work/lux/genesis/configs/mainnet/C/genesis.json $DATA_DIR/configs/chains/C/config.json

echo "======================================"
echo "Lux Mainnet - Local Validator Node"
echo "======================================"
echo "Network ID: 96369"
echo "Data Directory: $DATA_DIR (standard location)"
echo ""

# Get node ID
NODE_ID=$(./build/luxd --data-dir=$DATA_DIR --print-node-id 2>/dev/null || echo "NodeID-unknown")
echo "Node ID: $NODE_ID"
echo ""

# Launch luxd with local validator configuration
# Single validator mode with sybil protection disabled
echo "Starting node..."
./build/luxd \
  --network-id=96369 \
  --data-dir=$DATA_DIR \
  --genesis-file=/home/z/work/lux/genesis/configs/local-validator/genesis.json \
  --http-host=0.0.0.0 \
  --public-ip=127.0.0.1 \
  --sybil-protection-enabled=false \
  --bootstrap-ips="" \
  --bootstrap-ids="" \
  --log-level=info \
  --api-admin-enabled \
  --api-metrics-enabled \
  --index-enabled \
  --chain-config-dir=$DATA_DIR/configs/chains > $DATA_DIR/node.log 2>&1 &

echo "Node PID: $!"
echo ""
echo "Waiting for node to start..."
sleep 10

# Check if node is running
if pgrep luxd > /dev/null; then
    echo "✅ Node is running"
    echo ""
    
    # Check network info
    echo "Network Info:"
    curl -s -X POST --data '{"jsonrpc":"2.0","method":"info.getNetworkID","params":{},"id":1}' \
         -H 'content-type:application/json;' http://127.0.0.1:9630/ext/info 2>/dev/null | python3 -m json.tool
    
    echo ""
    echo "Node ID:"
    curl -s -X POST --data '{"jsonrpc":"2.0","method":"info.getNodeID","params":{},"id":1}' \
         -H 'content-type:application/json;' http://127.0.0.1:9630/ext/info 2>/dev/null | python3 -m json.tool
    
    echo ""
    echo "Checking C-Chain..."
    sleep 5
    
    # Try to get C-Chain balance
    echo "Test Account Balance (0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC):"
    curl -s -X POST --data '{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC", "latest"],"id":1}' \
         -H 'content-type:application/json;' http://127.0.0.1:9630/ext/bc/C/rpc 2>/dev/null || echo "C-Chain not ready yet"
    
    echo ""
    echo "Node is running! Check logs at: $DATA_DIR/node.log"
    echo "API endpoint: http://127.0.0.1:9630"
else
    echo "❌ Node failed to start. Check logs:"
    tail -20 $DATA_DIR/node.log
fi