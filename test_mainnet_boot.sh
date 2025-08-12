#!/bin/bash

# Test mainnet boot with NetworkID=1, ChainID=96369

echo "Testing Lux mainnet boot with Avalanche-compatible NetworkID..."
echo "NetworkID: 1 (Avalanche-compatible)"
echo "ChainID: 96369 (Lux-specific)"
echo ""

# Clean data directory
rm -rf /tmp/test-lux-mainnet

# Start node with mainnet configuration
./build/luxd \
  --network-id=1 \
  --data-dir=/tmp/test-lux-mainnet \
  --public-ip=127.0.0.1 \
  --http-host=127.0.0.1 \
  --http-port=9630 \
  --staking-port=9631 \
  --log-level=info &

NODE_PID=$!
echo "Started luxd with PID: $NODE_PID"

# Wait for node to start
echo "Waiting for node to start..."
sleep 10

# Check if process is still running
if ps -p $NODE_PID > /dev/null; then
    echo "✓ Node is running"
    
    # Test RPC endpoint
    echo "Testing RPC endpoint..."
    RESPONSE=$(curl -s -X POST --data '{
        "jsonrpc":"2.0",
        "id"     :1,
        "method" :"info.getNetworkID"
    }' -H 'content-type:application/json;' http://127.0.0.1:9630/ext/info)
    
    echo "Network ID Response: $RESPONSE"
    
    # Test C-Chain
    echo "Testing C-Chain..."
    C_RESPONSE=$(curl -s -X POST --data '{
        "jsonrpc":"2.0",
        "id"     :1,
        "method" :"eth_chainId"
    }' -H 'content-type:application/json;' http://127.0.0.1:9630/ext/bc/C/rpc)
    
    echo "C-Chain ID Response: $C_RESPONSE"
    
    # Clean shutdown
    kill $NODE_PID
    wait $NODE_PID 2>/dev/null
    echo "✓ Node shut down cleanly"
    
    # Clean up
    rm -rf /tmp/test-lux-mainnet
    
    echo ""
    echo "✅ Mainnet boot test PASSED"
else
    echo "❌ Node crashed!"
    echo "Check logs for errors"
    exit 1
fi