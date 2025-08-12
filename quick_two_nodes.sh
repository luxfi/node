#!/bin/bash
set -e

echo "============================================"
echo "  QUICK TWO-NODE SETUP WITH SKIP-BOOTSTRAP"
echo "============================================"

# Kill any existing
pkill -f luxd || true
sleep 1

# Node A
echo "Starting Node A..."
/home/z/work/lux/build/luxd \
  --network-id=96369 \
  --public-ip=127.0.0.1 \
  --http-host=0.0.0.0 --http-port=9630 \
  --staking-port=9651 \
  --consensus-sample-size=1 --consensus-quorum-size=1 \
  --chain-data-dir=/home/z/.luxd/chainData \
  --db-dir=/home/z/.node/db/quick-nodeA \
  --skip-bootstrap \
  --log-level=info > /tmp/quick-nodeA.log 2>&1 &

NODE_A_PID=$!
echo "Node A PID: $NODE_A_PID"
sleep 3

# Get Node A ID
NODE_A_ID=$(curl -s http://127.0.0.1:9630/ext/info -X POST -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID","params":{}}' | jq -r '.result.nodeID')
echo "Node A ID: $NODE_A_ID"

# Node B
echo "Starting Node B..."
/home/z/work/lux/build/luxd \
  --network-id=96369 \
  --public-ip=127.0.0.1 \
  --staking-port=9653 --http-port=9632 \
  --bootstrap-ips=127.0.0.1:9651 \
  --bootstrap-ids=$NODE_A_ID \
  --consensus-sample-size=1 --consensus-quorum-size=1 \
  --chain-data-dir=/home/z/.luxd/chainData \
  --db-dir=/home/z/.node/db/quick-nodeB \
  --skip-bootstrap \
  --log-level=info > /tmp/quick-nodeB.log 2>&1 &

NODE_B_PID=$!
echo "Node B PID: $NODE_B_PID"
sleep 3

# Check status
echo ""
echo "Checking bootstrap status..."
for port in 9630 9632; do
  echo -n "Node on port $port: "
  curl -s -H 'content-type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"info.isBootstrapped","params":{"chain":"C"}}' \
    http://127.0.0.1:$port/ext/info | jq -r '.result.isBootstrapped'
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
  
  if [ $BLOCK_NUM -eq 1082780 ]; then
    echo "✅ SUCCESS! Full chain loaded: 1,082,780 blocks"
    
    # Check treasury balance
    echo ""
    echo "Checking treasury balance..."
    ADDR="0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"
    BALANCE_HEX=$(curl -s -H 'content-type: application/json' \
      -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_getBalance\",\"params\":[\"$ADDR\", \"latest\"]}" \
      http://127.0.0.1:9630/ext/bc/C/rpc | jq -r '.result')
    
    if [ "$BALANCE_HEX" != "null" ] && [ "$BALANCE_HEX" != "" ]; then
      # Convert to decimal
      BALANCE_WEI=$(printf "%d" "$BALANCE_HEX")
      echo "Balance in Wei: $BALANCE_WEI"
      # Rough conversion to LUX (divide by 10^18)
      echo "Approximate LUX: $(echo "scale=2; $BALANCE_WEI / 1000000000000000000" | bc)"
    fi
  else
    echo "⚠️  Block number is $BLOCK_NUM - expected 1082780"
  fi
else
  echo "❌ Failed to get block number"
fi

echo ""
echo "Nodes running:"
echo "  Node A: http://127.0.0.1:9630 (PID: $NODE_A_PID)"
echo "  Node B: http://127.0.0.1:9632 (PID: $NODE_B_PID)"
echo ""
echo "Press Ctrl+C to stop"

trap "kill $NODE_A_PID $NODE_B_PID 2>/dev/null; exit" INT
wait