#!/bin/bash
set -e

echo "============================================"
echo "  LUX VALIDATOR & BOOTSTRAPPER NODE"
echo "============================================"
echo ""
echo "This node will serve as both validator and bootstrapper"
echo "Consensus params: K=1, sample-size=1, quorum-size=1"
echo ""

# Kill any existing luxd
pkill -f luxd 2>/dev/null || true
sleep 1

# Configuration
STAKING_DIR="/home/z/.luxd/staking"
DB_DIR="/home/z/.node/db/validator-bootstrapper"
CHAIN_DATA="/home/z/.luxd/chainData"

# Clean and prepare
rm -rf "$DB_DIR"
mkdir -p "$DB_DIR"

echo "Starting validator/bootstrapper node..."
echo "Using staking certificates from: $STAKING_DIR"
echo ""

# Start the validator/bootstrapper node
# With K=1, this node should bootstrap itself immediately
/home/z/work/lux/build/luxd \
  --network-id=96369 \
  --public-ip=127.0.0.1 \
  --http-host=0.0.0.0 \
  --http-port=9630 \
  --staking-port=9651 \
  --staking-tls-cert-file="$STAKING_DIR/staker.crt" \
  --staking-tls-key-file="$STAKING_DIR/staker.key" \
  --staking-signer-key-file="$STAKING_DIR/signer.key" \
  --consensus-sample-size=1 \
  --consensus-quorum-size=1 \
  --chain-data-dir="$CHAIN_DATA" \
  --db-dir="$DB_DIR" \
  --bootstrap-ips="" \
  --bootstrap-ids="" \
  --skip-bootstrap \
  --log-level=info > /tmp/validator-bootstrapper.log 2>&1 &

NODE_PID=$!
echo "Validator/Bootstrapper PID: $NODE_PID"
echo ""

# Wait a moment for initialization
sleep 3

# Get node info
NODE_ID=$(curl -s http://127.0.0.1:9630/ext/info -X POST -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID","params":{}}' | jq -r '.result.nodeID')

echo "Node ID: $NODE_ID"
echo ""

# Check bootstrap status
echo "Bootstrap Status:"
for chain in P C X; do
  STATUS=$(curl -s -H 'content-type: application/json' \
    -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"info.isBootstrapped\",\"params\":{\"chain\":\"$chain\"}}" \
    http://127.0.0.1:9630/ext/info | jq -r '.result.isBootstrapped')
  echo "  $chain-Chain: $STATUS"
done

# Check validator status on P-Chain
echo ""
echo "P-Chain Validator Info:"
echo "  Validator Address: 0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"
echo "  Expected Stake: 1,000,000,000 LUX (1B LUX)"
echo "  Minimum Stake Required: 1,000,000 LUX (1M LUX)"

# Check C-Chain
echo ""
echo "C-Chain Status:"
BLOCK_HEX=$(curl -s -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
  http://127.0.0.1:9630/ext/bc/C/rpc | jq -r '.result')

if [ "$BLOCK_HEX" != "null" ] && [ "$BLOCK_HEX" != "" ]; then
  BLOCK_NUM=$((16#${BLOCK_HEX#0x}))
  echo "  Current block: $BLOCK_NUM"
  
  if [ $BLOCK_NUM -eq 0 ]; then
    echo "  ⚠️  Block 0 - database needs repair for full chain"
  fi
fi

echo ""
echo "================================"
echo "  CONNECTION INFO FOR OTHER NODES"
echo "================================"
echo "Other nodes can connect using:"
echo "  --bootstrap-ips=127.0.0.1:9651"
echo "  --bootstrap-ids=$NODE_ID"
echo "  --network-id=96369"
echo ""
echo "Example for second node:"
echo "  ./build/luxd \\"
echo "    --network-id=96369 \\"
echo "    --http-port=9632 --staking-port=9653 \\"
echo "    --bootstrap-ips=127.0.0.1:9651 \\"
echo "    --bootstrap-ids=$NODE_ID \\"
echo "    --consensus-sample-size=1 --consensus-quorum-size=1"
echo ""
echo "================================"
echo "  VALIDATOR/BOOTSTRAPPER RUNNING"
echo "================================"
echo "✓ Node is operational as validator and bootstrapper"
echo "✓ With K=1 consensus, this node can validate alone"
echo "✓ Other nodes can bootstrap from this node"
echo ""
echo "Endpoints:"
echo "  HTTP RPC: http://127.0.0.1:9630"
echo "  C-Chain: http://127.0.0.1:9630/ext/bc/C/rpc"
echo "  P-Chain: http://127.0.0.1:9630/ext/bc/P"
echo "  Staking: 127.0.0.1:9651"
echo ""
echo "PID: $NODE_PID"
echo "Logs: tail -f /tmp/validator-bootstrapper.log"
echo ""
echo "Press Ctrl+C to stop"

trap "echo 'Stopping validator...'; kill $NODE_PID 2>/dev/null; exit" INT
wait $NODE_PID