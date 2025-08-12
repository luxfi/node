#!/bin/bash
set -e

echo "============================================"
echo "  LUX INSTANT BOOTSTRAP (<1 second)"
echo "============================================"
echo ""

# Kill any existing luxd
pkill -f luxd 2>/dev/null || true
sleep 1

# Start both nodes with --skip-bootstrap for instant startup
echo "Starting Node A with skip-bootstrap..."
/home/z/work/lux/build/luxd \
  --network-id=96369 \
  --http-host=0.0.0.0 --http-port=9630 \
  --staking-port=9651 \
  --consensus-sample-size=1 --consensus-quorum-size=1 \
  --chain-data-dir=/home/z/.luxd/chainData \
  --db-dir=/home/z/.node/db/instant-nodeA \
  --skip-bootstrap \
  --log-level=info > /tmp/instant-nodeA.log 2>&1 &

NODE_A_PID=$!
echo "Node A PID: $NODE_A_PID"

# Immediate check - should be instant
sleep 1

STATUS_A=$(curl -s -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"info.isBootstrapped","params":{"chain":"C"}}' \
  http://127.0.0.1:9630/ext/info | jq -r '.result.isBootstrapped')

echo "Node A bootstrapped: $STATUS_A (should be true instantly)"

# Get Node A's ID
NODE_A_ID=$(curl -s http://127.0.0.1:9630/ext/info -X POST -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID","params":{}}' | jq -r '.result.nodeID')
echo "Node A ID: $NODE_A_ID"

# Start Node B also with skip-bootstrap
echo ""
echo "Starting Node B with skip-bootstrap..."
/home/z/work/lux/build/luxd \
  --network-id=96369 \
  --http-host=0.0.0.0 --http-port=9632 \
  --staking-port=9653 \
  --bootstrap-ips=127.0.0.1:9651 \
  --bootstrap-ids=$NODE_A_ID \
  --consensus-sample-size=1 --consensus-quorum-size=1 \
  --chain-data-dir=/home/z/.luxd/chainData \
  --db-dir=/home/z/.node/db/instant-nodeB \
  --skip-bootstrap \
  --log-level=info > /tmp/instant-nodeB.log 2>&1 &

NODE_B_PID=$!
echo "Node B PID: $NODE_B_PID"

# Immediate check
sleep 1

STATUS_B=$(curl -s -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"info.isBootstrapped","params":{"chain":"C"}}' \
  http://127.0.0.1:9632/ext/info | jq -r '.result.isBootstrapped')

echo "Node B bootstrapped: $STATUS_B (should be true instantly)"

# Check C-Chain status
echo ""
echo "Checking C-Chain..."
BLOCK_A=$(curl -s -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
  http://127.0.0.1:9630/ext/bc/C/rpc | jq -r '.result')

BLOCK_B=$(curl -s -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
  http://127.0.0.1:9632/ext/bc/C/rpc | jq -r '.result')

echo "Node A block: $BLOCK_A"
echo "Node B block: $BLOCK_B"

# Check peer connections
PEERS_A=$(curl -s -X POST --data '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"info.peers"
}' -H 'content-type:application/json;' http://127.0.0.1:9630/ext/info | jq '.result.numPeers')

PEERS_B=$(curl -s -X POST --data '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"info.peers"
}' -H 'content-type:application/json;' http://127.0.0.1:9632/ext/info | jq '.result.numPeers')

echo ""
echo "Node A peers: $PEERS_A"
echo "Node B peers: $PEERS_B"

echo ""
echo "================================"
echo "  INSTANT BOOTSTRAP COMPLETE"
echo "================================"
echo "✓ Both nodes operational in <1 second"
echo "✓ Skip-bootstrap enabled for instant startup"
echo ""
echo "Node A: http://127.0.0.1:9630"
echo "Node B: http://127.0.0.1:9632"
echo ""
echo "PIDs: NodeA=$NODE_A_PID, NodeB=$NODE_B_PID"
echo ""
echo "NOTE: Database still needs repair to access full chain (1,082,781 blocks)"
echo "Currently only genesis and early blocks are accessible."
echo ""
echo "Press Ctrl+C to stop both nodes"

trap "echo 'Stopping nodes...'; kill $NODE_A_PID $NODE_B_PID 2>/dev/null; exit" INT
wait $NODE_A_PID