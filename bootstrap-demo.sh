#!/bin/bash

# Lux Network Bootstrap Demo
# Shows how nodes discover and connect to each other

set -e

echo "🧹 Cleaning up any existing processes..."
killall -9 luxd 2>/dev/null || true
sleep 2

cd "$(dirname "$0")"

echo ""
echo "=== Starting Lux Network Bootstrap Demo ==="
echo ""

# Start master node
echo "🚀 Starting master node on port 9630..."
rm -rf /tmp/luxd-master
./build/luxd \
  --network-id=96369 \
  --http-port=9630 \
  --staking-port=9631 \
  --data-dir=/tmp/luxd-master \
  --http-host=0.0.0.0 \
  --public-ip=127.0.0.1 \
  --consensus-sample-size=1 \
  --consensus-quorum-size=1 \
  --log-level=info 2>&1 | tee /tmp/master.log &

MASTER_PID=$!
echo "   Master PID: $MASTER_PID"

# Wait for master to start
echo "⏳ Waiting for master node to initialize..."
sleep 5

# Get master node ID
MASTER_NODE_ID=$(curl -s -X POST --data '{"jsonrpc":"2.0","method":"info.getNodeID","params":{},"id":1}' \
  -H 'content-type:application/json;' http://localhost:9630/ext/info | jq -r '.result.nodeID')

echo "✅ Master node started with ID: $MASTER_NODE_ID"
echo ""

# Start peer node
echo "🚀 Starting peer node on port 9640..."
echo "   Bootstrapping from: $MASTER_NODE_ID at 127.0.0.1:9631"

rm -rf /tmp/luxd-peer
./build/luxd \
  --network-id=96369 \
  --http-port=9640 \
  --staking-port=9641 \
  --data-dir=/tmp/luxd-peer \
  --http-host=0.0.0.0 \
  --public-ip=127.0.0.1 \
  --bootstrap-ips=127.0.0.1:9631 \
  --bootstrap-ids=$MASTER_NODE_ID \
  --consensus-sample-size=1 \
  --consensus-quorum-size=1 \
  --log-level=info 2>&1 | tee /tmp/peer.log &

PEER_PID=$!
echo "   Peer PID: $PEER_PID"

# Wait for peer to connect
echo "⏳ Waiting for peer to connect..."
sleep 5

# Get peer node ID
PEER_NODE_ID=$(curl -s -X POST --data '{"jsonrpc":"2.0","method":"info.getNodeID","params":{},"id":1}' \
  -H 'content-type:application/json;' http://localhost:9640/ext/info | jq -r '.result.nodeID')

echo "✅ Peer node started with ID: $PEER_NODE_ID"
echo ""

# Check connection status
echo "📊 Checking network status..."
echo ""

echo "Master node peers:"
curl -s -X POST --data '{"jsonrpc":"2.0","method":"info.peers","params":{},"id":1}' \
  -H 'content-type:application/json;' http://localhost:9630/ext/info | jq '.result | {numPeers, peers: .peers[0:2]}'

echo ""
echo "Peer node peers:"
curl -s -X POST --data '{"jsonrpc":"2.0","method":"info.peers","params":{},"id":1}' \
  -H 'content-type:application/json;' http://localhost:9640/ext/info | jq '.result | {numPeers, peers: .peers[0:2]}'

echo ""
echo "✅ Bootstrap successful! Nodes are connected and running."
echo ""
echo "📝 Endpoints:"
echo "   Master: http://localhost:9630"
echo "   Peer:   http://localhost:9640"
echo ""
echo "   C-Chain RPC (Master): http://localhost:9630/ext/bc/C/rpc"
echo "   C-Chain RPC (Peer):   http://localhost:9640/ext/bc/C/rpc"
echo ""
echo "🛑 To stop: killall luxd"
echo ""
echo "Nodes are running in background. Check logs at:"
echo "   /tmp/master.log"
echo "   /tmp/peer.log"