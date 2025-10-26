#!/bin/bash
# Launch Lux node in mainnet mode with single validator configuration
# No --dev flag, but with consensus parameters for single validator

cd /home/z/work/lux/node

export CORETH_DISABLE_FREEZER=1

./build/luxd \
  --network-id=96369 \
  --http-host=0.0.0.0 \
  --http-port=9630 \
  --staking-port=9631 \
  --data-dir=/home/z/.luxd \
  --chain-data-dir=/home/z/.luxd/chainData \
  --db-dir=/home/z/.luxd/db \
  --log-level=info \
  --sybil-protection-enabled=false \
  --sybil-protection-disabled-weight=100 \
  --consensus-sample-size=1 \
  --consensus-quorum-size=1 \
  --network-health-min-conn-peers=0 \
  --bootstrap-ips="" \
  --bootstrap-ids="" \
  > /home/z/.luxd/node_mainnet.log 2>&1 &

echo "Node launched with PID: $!"
echo "Log file: /home/z/.luxd/node_mainnet.log"
echo "Waiting for node to start..."
sleep 5

# Check if node is running
if ps -p $! > /dev/null; then
    echo "Node is running"
    echo "Testing Platform API..."
    curl -s -X POST -H 'Content-Type: application/json' \
      -d '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID","params":{}}' \
      http://127.0.0.1:9630/ext/info | jq .
    
    echo "Checking C-Chain status..."
    curl -s -X POST -H 'Content-Type: application/json' \
      -d '{"jsonrpc":"2.0","id":1,"method":"info.getBlockchainID","params":{"alias":"C"}}' \
      http://127.0.0.1:9630/ext/info | jq .
else
    echo "Node failed to start. Check logs at /home/z/.luxd/node_mainnet.log"
    tail -20 /home/z/.luxd/node_mainnet.log
fi