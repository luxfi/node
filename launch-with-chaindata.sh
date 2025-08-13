#!/bin/bash

# Standard Lux data directory
DATA_DIR=~/.luxd

# Kill any existing luxd process
pkill luxd 2>/dev/null
sleep 2

echo "======================================" 
echo "Lux Mainnet with Existing Chain Data"
echo "======================================"
echo "Network ID: 96369"
echo "Data Directory: $DATA_DIR"
echo ""

# Check if chainData exists
CHAIN_DATA_DIR=~/work/lux/chainData
if [ -d "$CHAIN_DATA_DIR" ]; then
    echo "✅ Found chainData directory at $CHAIN_DATA_DIR"
    echo "Checking chain data..."
    
    # List chain directories
    for chain_dir in $CHAIN_DATA_DIR/*/; do
        if [ -d "$chain_dir" ]; then
            chain_id=$(basename "$chain_dir")
            echo "  Chain: $chain_id"
            
            # Check for database
            if [ -d "$chain_dir/db/pebbledb" ]; then
                echo "    ✓ PebbleDB found"
                # Try to get block count or size
                if [ -f "$chain_dir/db/pebbledb/CURRENT" ]; then
                    echo "    ✓ Database is initialized"
                fi
            fi
        fi
    done
else
    echo "⚠️  No chainData directory found at $CHAIN_DATA_DIR"
    echo "Please ensure chain data is available at this location"
    exit 1
fi

echo ""
echo "Starting node with existing chain data..."

# Launch luxd with chain data
# No genesis file needed when using existing data
./build/luxd \
  --network-id=96369 \
  --data-dir=$DATA_DIR \
  --chain-data-dir=~/work/lux/chainData \
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

PID=$!
echo "Node PID: $PID"
echo ""
echo "Waiting for node to start..."
sleep 15

# Check if node is running
if pgrep luxd > /dev/null; then
    echo "✅ Node is running"
    echo ""
    
    # Check network info
    echo "Network Info:"
    curl -s -X POST --data '{"jsonrpc":"2.0","method":"info.getNetworkID","params":{},"id":1}' \
         -H 'content-type:application/json;' http://127.0.0.1:9630/ext/info 2>/dev/null | python3 -m json.tool
    
    echo ""
    echo "Checking C-Chain status..."
    
    # Get block number
    echo "Current block number:"
    curl -s -X POST --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
         -H 'content-type:application/json;' http://127.0.0.1:9630/ext/bc/C/rpc 2>/dev/null || echo "C-Chain not ready yet"
    
    echo ""
    echo "Getting chain info..."
    curl -s -X POST --data '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' \
         -H 'content-type:application/json;' http://127.0.0.1:9630/ext/bc/C/rpc 2>/dev/null || echo "C-Chain not ready yet"
    
    echo ""
    echo "Node is running! Check logs at: $DATA_DIR/node.log"
    echo "API endpoint: http://127.0.0.1:9630"
    echo ""
    echo "To check a balance, use:"
    echo "curl -X POST --data '{\"jsonrpc\":\"2.0\",\"method\":\"eth_getBalance\",\"params\":[\"<ADDRESS>\", \"latest\"],\"id\":1}' -H 'content-type:application/json;' http://127.0.0.1:9630/ext/bc/C/rpc"
else
    echo "❌ Node failed to start. Check logs:"
    tail -50 $DATA_DIR/node.log
fi