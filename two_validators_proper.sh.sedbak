#!/bin/bash
set -e

echo "============================================"
echo "  LUX TWO VALIDATOR PROPER SETUP"
echo "============================================"
echo ""

# Kill any existing luxd
pkill -f luxd 2>/dev/null || true
sleep 2

# Start Node A (bootstrapper)
echo "Starting Node A (bootstrapper)..."
/home/z/work/lux/build/luxd \
  --network-id=96369 \
  --public-ip=127.0.0.1 \
  --http-host=0.0.0.0 --http-port=9630 \
  --staking-port=9651 \
  --consensus-sample-size=1 --consensus-quorum-size=1 \
  --chain-data-dir=/home/z/.luxd/chainData \
  --db-dir=/home/z/.node/db/lux-mainnet-96369-nodeA \
  --log-level=debug > /tmp/nodeA.log 2>&1 &

NODE_A_PID=$!
echo "Node A PID: $NODE_A_PID"
echo "Waiting for Node A to initialize..."
sleep 5

# Get Node A's ID
NODE_A_ID=$(curl -s http://127.0.0.1:9630/ext/info -X POST -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID","params":{}}' | jq -r '.result.nodeID')

echo "Node A ID: $NODE_A_ID"
echo ""

# Start Node B (helper)
echo "Starting Node B (helper)..."
/home/z/work/lux/build/luxd \
  --network-id=96369 \
  --public-ip=127.0.0.1 \
  --staking-port=9653 --http-port=9632 \
  --bootstrap-ips=127.0.0.1:9651 \
  --bootstrap-ids=$NODE_A_ID \
  --consensus-sample-size=1 --consensus-quorum-size=1 \
  --chain-data-dir=/home/z/.luxd/chainData \
  --db-dir=/home/z/.node/db/lux-mainnet-96369-nodeB \
  --log-level=debug > /tmp/nodeB.log 2>&1 &

NODE_B_PID=$!
echo "Node B PID: $NODE_B_PID"
echo "Waiting for bootstrap to complete..."
sleep 10

# Check bootstrap status
echo ""
echo "Checking bootstrap status..."
for i in {1..30}; do
  STATUS_A=$(curl -s -H 'content-type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"info.isBootstrapped","params":{"chain":"C"}}' \
    http://127.0.0.1:9630/ext/info | jq -r '.result.isBootstrapped')
  
  STATUS_B=$(curl -s -H 'content-type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"info.isBootstrapped","params":{"chain":"C"}}' \
    http://127.0.0.1:9632/ext/info | jq -r '.result.isBootstrapped')
  
  echo "Attempt $i/30: Node A bootstrapped=$STATUS_A, Node B bootstrapped=$STATUS_B"
  
  if [ "$STATUS_A" = "true" ] && [ "$STATUS_B" = "true" ]; then
    echo "✓ Both nodes bootstrapped successfully!"
    break
  fi
  
  sleep 2
done

# Check block number
echo ""
echo "Checking C-Chain block number..."
BLOCK_HEX=$(curl -s -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
  http://127.0.0.1:9630/ext/bc/C/rpc | jq -r '.result')

if [ "$BLOCK_HEX" != "null" ] && [ "$BLOCK_HEX" != "" ]; then
  BLOCK_NUM=$((16#${BLOCK_HEX#0x}))
  echo "Current block: $BLOCK_NUM (hex: $BLOCK_HEX)"
  
  if [ $BLOCK_NUM -eq 0 ]; then
    echo "⚠️  WARNING: Block number is 0 - database needs repair!"
    echo "The canonical chain mappings are incomplete."
    echo "Need to run database repair before blocks will be accessible."
  elif [ $BLOCK_NUM -eq 1082780 ]; then
    echo "✓ Full chain loaded: 1,082,780 blocks"
  else
    echo "Partial chain loaded: $BLOCK_NUM blocks"
  fi
fi

echo ""
echo "================================"
echo "  NETWORK STATUS"
echo "================================"
echo "Node A: http://127.0.0.1:9630"
echo "Node B: http://127.0.0.1:9632"
echo ""
echo "You can now stop Node B if desired (it was just for bootstrap):"
echo "  kill $NODE_B_PID"
echo ""
echo "Node A will remain bootstrapped and operational."
echo ""
echo "PIDs: NodeA=$NODE_A_PID, NodeB=$NODE_B_PID"
echo "Logs: tail -f /tmp/nodeA.log or /tmp/nodeB.log"
echo ""
echo "Press Ctrl+C to stop both nodes"

trap "echo 'Stopping nodes...'; kill $NODE_A_PID $NODE_B_PID 2>/dev/null; exit" INT
wait $NODE_A_PID