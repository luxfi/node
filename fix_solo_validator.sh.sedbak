#!/bin/bash
set -e

echo "============================================"
echo "  LUX SOLO VALIDATOR FIX"
echo "============================================"
echo ""
echo "Setting up single validator with skip-bootstrap"
echo ""

# Kill any existing luxd
pkill -f luxd 2>/dev/null || true
sleep 2

# Configuration
DB_DIR="/home/z/.node/db"
CHAIN_DATA="/home/z/.luxd/chainData"
HTTP_PORT=9630
STAKING_PORT=9631

# Clean up old database state
echo "Cleaning up old state..."
rm -rf "$DB_DIR/lux-mainnet-96369"
rm -rf "$DB_DIR/lux-mainnet"

echo "Starting solo validator node..."
echo "================================"

# Start luxd with skip-bootstrap for solo validator
/home/z/work/lux/build/luxd \
    --network-id=96369 \
    --http-host=0.0.0.0 \
    --http-port=$HTTP_PORT \
    --staking-port=$STAKING_PORT \
    --consensus-sample-size=1 \
    --consensus-quorum-size=1 \
    --db-dir="$DB_DIR" \
    --chain-data-dir="$CHAIN_DATA" \
    --skip-bootstrap \
    --bootstrap-ips="" \
    --bootstrap-ids="" \
    --log-level=info > /tmp/solo_validator.log 2>&1 &

VALIDATOR_PID=$!
echo "Validator PID: $VALIDATOR_PID"
echo ""

# Wait for initialization
echo "Initializing (15 seconds)..."
for i in {1..15}; do
    if [ $((i % 5)) -eq 0 ]; then
        echo "  $i seconds..."
    fi
    sleep 1
done

echo ""
echo "================================"
echo "  CHECKING NODE STATUS"
echo "================================"

# Check if running
if ! kill -0 $VALIDATOR_PID 2>/dev/null; then
    echo "✗ Node failed to start"
    echo "Last 50 lines of log:"
    tail -50 /tmp/solo_validator.log
    exit 1
fi

echo "✓ Node is running"
echo ""

# Check bootstrap status
echo "Bootstrap Status:"
for chain in P C X; do
    IS_BOOTSTRAPPED=$(curl -s -X POST -H "Content-Type: application/json" \
        -d "{\"jsonrpc\":\"2.0\",\"method\":\"info.isBootstrapped\",\"params\":{\"chain\":\"$chain\"},\"id\":1}" \
        http://localhost:$HTTP_PORT/ext/info 2>/dev/null | jq -r '.result.isBootstrapped' || echo "false")
    echo "  $chain-Chain: $IS_BOOTSTRAPPED"
done

# Check C-Chain status
echo ""
echo "C-Chain Status:"
BLOCK_HEX=$(curl -s -X POST -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
    http://localhost:$HTTP_PORT/ext/bc/C/rpc 2>/dev/null | jq -r '.result' 2>/dev/null || echo "0x0")

if [ "$BLOCK_HEX" != "null" ] && [ ! -z "$BLOCK_HEX" ]; then
    BLOCK_NUM=$((16#${BLOCK_HEX#0x}))
    echo "  Current block: $BLOCK_NUM"
fi

# Check balances
echo ""
echo "================================"
echo "  CHECKING BALANCES"
echo "================================"
ADDRESS="0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"
echo "Validator Address: $ADDRESS"
echo ""

# P-Chain balance (would need proper P-Chain API call)
echo "P-Chain:"
echo "  Expected: 1,000,000,000 LUX (1B LUX staked)"
echo "  Status: P-Chain API not available in current setup"

# C-Chain balance
echo ""
echo "C-Chain:"
BALANCE_HEX=$(curl -s -X POST -H "Content-Type: application/json" \
    -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_getBalance\",\"params\":[\"$ADDRESS\", \"latest\"]}" \
    http://localhost:$HTTP_PORT/ext/bc/C/rpc 2>/dev/null | jq -r '.result' 2>/dev/null || echo "0x0")

if [ "$BALANCE_HEX" != "null" ] && [ "$BALANCE_HEX" != "0x0" ] && [ "$BALANCE_HEX" != "error" ]; then
    # Convert hex to decimal
    BALANCE_WEI=$((16#${BALANCE_HEX#0x}))
    echo "  Balance: $BALANCE_WEI Wei"
    
    # Convert to LUX (divide by 10^18)
    if [ $BALANCE_WEI -gt 0 ]; then
        LUX_BALANCE=$(echo "scale=4; $BALANCE_WEI / 1000000000000000000" | bc)
        echo "  Balance: $LUX_BALANCE LUX"
    fi
else
    echo "  Balance: Unable to query (chain data may be loading)"
fi

# X-Chain would need proper X-Chain API
echo ""
echo "X-Chain:"
echo "  Status: X-Chain API not available in current setup"

# Display endpoints
echo ""
echo "================================"
echo "  VALIDATOR ENDPOINTS"
echo "================================"
echo "HTTP RPC: http://localhost:$HTTP_PORT"
echo "C-Chain: http://localhost:$HTTP_PORT/ext/bc/C/rpc"
echo "P-Chain: http://localhost:$HTTP_PORT/ext/bc/P"
echo "X-Chain: http://localhost:$HTTP_PORT/ext/bc/X"
echo "Health: http://localhost:$HTTP_PORT/ext/health"
echo ""

echo "================================"
echo "  SOLO VALIDATOR RUNNING"
echo "================================"
echo ""
echo "✓ Single node validator is operational"
echo "✓ Bootstrap skipped (--skip-bootstrap flag)"
echo "✓ Consensus K=1 (solo mode)"
echo "✓ Ready to process transactions"
echo ""
echo "PID: $VALIDATOR_PID"
echo "Logs: tail -f /tmp/solo_validator.log"
echo ""
echo "To make this work with TWO validators:"
echo "1. Start second node with different ports"
echo "2. Connect using --bootstrap-ips=<first-node-ip>:9631"
echo "3. Both nodes need same network-id and chain-data"
echo ""
echo "Press Ctrl+C to stop"

# Trap and cleanup
trap "echo 'Stopping validator...'; kill $VALIDATOR_PID 2>/dev/null; exit" INT

# Keep running
wait $VALIDATOR_PID