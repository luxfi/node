#!/bin/bash
set -e

echo "============================================"
echo "  LUX TWO VALIDATOR SETUP"
echo "============================================"
echo ""
echo "Setting up two validators with K=1 consensus"
echo ""

# Kill any existing luxd
pkill -f luxd 2>/dev/null || true
sleep 2

# Configuration
DB_DIR_1="/home/z/.node/db-validator1"
DB_DIR_2="/home/z/.node/db-validator2"
CHAIN_DATA="/home/z/.luxd/chainData"
HTTP_PORT_1=9630
STAKING_PORT_1=9631
HTTP_PORT_2=9640
STAKING_PORT_2=9641

# Clean up old database state
echo "Cleaning up old state..."
rm -rf "$DB_DIR_1"
rm -rf "$DB_DIR_2"
mkdir -p "$DB_DIR_1"
mkdir -p "$DB_DIR_2"

echo "Starting first validator node..."
echo "================================"

# Start first validator
/home/z/work/lux/build/luxd \
    --network-id=96369 \
    --http-host=0.0.0.0 \
    --http-port=$HTTP_PORT_1 \
    --staking-port=$STAKING_PORT_1 \
    --consensus-sample-size=1 \
    --consensus-quorum-size=1 \
    --db-dir="$DB_DIR_1" \
    --chain-data-dir="$CHAIN_DATA" \
    --skip-bootstrap \
    --bootstrap-ips="" \
    --bootstrap-ids="" \
    --log-level=info > /tmp/validator1.log 2>&1 &

VALIDATOR1_PID=$!
echo "Validator 1 PID: $VALIDATOR1_PID"
echo "  HTTP Port: $HTTP_PORT_1"
echo "  Staking Port: $STAKING_PORT_1"
echo ""

# Wait for first validator to initialize
echo "Waiting for first validator to initialize (10 seconds)..."
sleep 10

# Get Node ID of first validator
NODE1_ID=$(curl -s -X POST --data '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"info.getNodeID"
}' -H 'content-type:application/json;' http://localhost:$HTTP_PORT_1/ext/info 2>/dev/null | jq -r '.result.nodeID' || echo "unknown")

echo "First validator Node ID: $NODE1_ID"
echo ""

echo "Starting second validator node..."
echo "================================"

# Start second validator, connecting to first
/home/z/work/lux/build/luxd \
    --network-id=96369 \
    --http-host=0.0.0.0 \
    --http-port=$HTTP_PORT_2 \
    --staking-port=$STAKING_PORT_2 \
    --consensus-sample-size=1 \
    --consensus-quorum-size=1 \
    --db-dir="$DB_DIR_2" \
    --chain-data-dir="$CHAIN_DATA" \
    --skip-bootstrap \
    --bootstrap-ips="127.0.0.1:$STAKING_PORT_1" \
    --bootstrap-ids="$NODE1_ID" \
    --log-level=info > /tmp/validator2.log 2>&1 &

VALIDATOR2_PID=$!
echo "Validator 2 PID: $VALIDATOR2_PID"
echo "  HTTP Port: $HTTP_PORT_2"
echo "  Staking Port: $STAKING_PORT_2"
echo ""

# Wait for second validator to initialize
echo "Waiting for second validator to initialize (10 seconds)..."
sleep 10

# Get Node ID of second validator
NODE2_ID=$(curl -s -X POST --data '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"info.getNodeID"
}' -H 'content-type:application/json;' http://localhost:$HTTP_PORT_2/ext/info 2>/dev/null | jq -r '.result.nodeID' || echo "unknown")

echo "Second validator Node ID: $NODE2_ID"
echo ""

echo "================================"
echo "  CHECKING NETWORK STATUS"
echo "================================"

# Check if both are running
if ! kill -0 $VALIDATOR1_PID 2>/dev/null; then
    echo "✗ Validator 1 failed to start"
    echo "Last 50 lines of log:"
    tail -50 /tmp/validator1.log
    exit 1
fi

if ! kill -0 $VALIDATOR2_PID 2>/dev/null; then
    echo "✗ Validator 2 failed to start"
    echo "Last 50 lines of log:"
    tail -50 /tmp/validator2.log
    exit 1
fi

echo "✓ Both validators are running"
echo ""

# Check bootstrap status for both
echo "Validator 1 Bootstrap Status:"
for chain in P C X; do
    IS_BOOTSTRAPPED=$(curl -s -X POST -H "Content-Type: application/json" \
        -d "{\"jsonrpc\":\"2.0\",\"method\":\"info.isBootstrapped\",\"params\":{\"chain\":\"$chain\"},\"id\":1}" \
        http://localhost:$HTTP_PORT_1/ext/info 2>/dev/null | jq -r '.result.isBootstrapped' || echo "false")
    echo "  $chain-Chain: $IS_BOOTSTRAPPED"
done

echo ""
echo "Validator 2 Bootstrap Status:"
for chain in P C X; do
    IS_BOOTSTRAPPED=$(curl -s -X POST -H "Content-Type: application/json" \
        -d "{\"jsonrpc\":\"2.0\",\"method\":\"info.isBootstrapped\",\"params\":{\"chain\":\"$chain\"},\"id\":1}" \
        http://localhost:$HTTP_PORT_2/ext/info 2>/dev/null | jq -r '.result.isBootstrapped' || echo "false")
    echo "  $chain-Chain: $IS_BOOTSTRAPPED"
done

# Check peer connections
echo ""
echo "Peer Connections:"
PEERS1=$(curl -s -X POST --data '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"info.peers"
}' -H 'content-type:application/json;' http://localhost:$HTTP_PORT_1/ext/info 2>/dev/null | jq '.result.numPeers' || echo "0")

PEERS2=$(curl -s -X POST --data '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"info.peers"
}' -H 'content-type:application/json;' http://localhost:$HTTP_PORT_2/ext/info 2>/dev/null | jq '.result.numPeers' || echo "0")

echo "  Validator 1 peers: $PEERS1"
echo "  Validator 2 peers: $PEERS2"

# Check C-Chain status on both
echo ""
echo "C-Chain Status:"
BLOCK1=$(curl -s -X POST -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
    http://localhost:$HTTP_PORT_1/ext/bc/C/rpc 2>/dev/null | jq -r '.result' 2>/dev/null || echo "0x0")

BLOCK2=$(curl -s -X POST -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
    http://localhost:$HTTP_PORT_2/ext/bc/C/rpc 2>/dev/null | jq -r '.result' 2>/dev/null || echo "0x0")

if [ "$BLOCK1" != "null" ] && [ ! -z "$BLOCK1" ]; then
    BLOCK_NUM1=$((16#${BLOCK1#0x}))
    echo "  Validator 1 block: $BLOCK_NUM1"
fi

if [ "$BLOCK2" != "null" ] && [ ! -z "$BLOCK2" ]; then
    BLOCK_NUM2=$((16#${BLOCK2#0x}))
    echo "  Validator 2 block: $BLOCK_NUM2"
fi

# Display endpoints
echo ""
echo "================================"
echo "  VALIDATOR ENDPOINTS"
echo "================================"
echo "Validator 1:"
echo "  Node ID: $NODE1_ID"
echo "  HTTP RPC: http://localhost:$HTTP_PORT_1"
echo "  C-Chain: http://localhost:$HTTP_PORT_1/ext/bc/C/rpc"
echo "  Staking: localhost:$STAKING_PORT_1"
echo ""
echo "Validator 2:"
echo "  Node ID: $NODE2_ID"
echo "  HTTP RPC: http://localhost:$HTTP_PORT_2"
echo "  C-Chain: http://localhost:$HTTP_PORT_2/ext/bc/C/rpc"
echo "  Staking: localhost:$STAKING_PORT_2"
echo ""

echo "================================"
echo "  TWO VALIDATORS RUNNING"
echo "================================"
echo ""
echo "✓ Two validator nodes are operational"
echo "✓ Bootstrap skipped (--skip-bootstrap flag)"
echo "✓ Consensus K=1 (minimum validators)"
echo "✓ Network ID: 96369"
echo "✓ Ready for consensus operations"
echo ""
echo "PIDs: Validator1=$VALIDATOR1_PID, Validator2=$VALIDATOR2_PID"
echo "Logs:"
echo "  tail -f /tmp/validator1.log"
echo "  tail -f /tmp/validator2.log"
echo ""
echo "Press Ctrl+C to stop both validators"

# Trap and cleanup
trap "echo 'Stopping validators...'; kill $VALIDATOR1_PID $VALIDATOR2_PID 2>/dev/null; exit" INT

# Keep running
wait $VALIDATOR1_PID $VALIDATOR2_PID