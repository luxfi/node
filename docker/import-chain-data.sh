#!/bin/bash
set -e

# Script to import subnet-evm data into C-Chain after network is running
# This is run inside the Docker container

SOURCE_DB="/import/chainData"
CONFIG_FILE="/luxd/configs/chains/C/config.json"
GENESIS_FILE="/luxd/configs/chains/C/genesis.json"

echo "=== C-Chain Data Import Script ==="
echo ""

# Step 1: Verify source database exists
if [ ! -d "$SOURCE_DB/pebbledb" ]; then
    echo "❌ Error: Source database not found at $SOURCE_DB/pebbledb"
    exit 1
fi

echo "✅ Found source subnet-evm database"
SIZE=$(du -sh "$SOURCE_DB/pebbledb" | cut -f1)
echo "   Database size: $SIZE"

# Step 2: Check if network is healthy
echo ""
echo "Checking network health..."
HEALTH_RESPONSE=$(curl -s http://localhost:9650/ext/health || echo "{}")

if ! echo "$HEALTH_RESPONSE" | grep -q "healthy"; then
    echo "❌ Network is not healthy yet. Please wait for network to start."
    exit 1
fi

echo "✅ Network is healthy"

# Step 3: Stop the node to perform import
echo ""
echo "Stopping node to perform import..."
pkill -SIGTERM luxd
sleep 5

# Step 4: Update C-Chain config with import directive
echo ""
echo "Updating C-Chain config for import..."
cat > "$CONFIG_FILE" <<EOF
{
  "snowman-api-enabled": false,
  "coreth-admin-api-enabled": true,
  "eth-apis": ["eth", "eth-filter", "net", "web3", "internal-eth", "internal-blockchain", "internal-debug", "internal-tx-pool", "debug", "trace"],
  "personal-api-enabled": true,
  "import-chain-data": "$SOURCE_DB",
  "database-backend": "pebbledb",
  "local-txs-enabled": true,
  "api-max-duration": 0,
  "api-max-blocks-per-request": 0,
  "api-max-gas-per-request": 0,
  "ws-enabled": true,
  "ws-port": 8546,
  "allow-unfinalized-queries": true,
  "log-level": "info",
  "state-sync-enabled": false,
  "pruning-enabled": false,
  "tx-pool-price-limit": 1,
  "tx-pool-account-slots": 16,
  "tx-pool-global-slots": 4096,
  "tx-pool-account-queue": 64,
  "tx-pool-global-queue": 1024,
  "rpc-gas-cap": 0,
  "rpc-tx-fee-cap": 0
}
EOF

# Step 5: Restart node with import
echo ""
echo "Restarting node to import chain data..."
echo "This may take several minutes to process 1M blocks..."

# Build command arguments
CMD_ARGS=(
    "--data-dir=/luxd"
    "--network-id=${NETWORK_ID:-96369}"
    "--http-host=0.0.0.0"
    "--http-port=9650"
    "--chain-config-dir=/luxd/configs/chains"
    "--index-allow-incomplete"
    "--force-ignore-checksum"
    "--log-level=info"
)

# Add conditional arguments
if [ "${DEV_MODE:-true}" = "true" ]; then
    CMD_ARGS+=("--dev")
fi

if [ "${API_ADMIN_ENABLED:-true}" = "true" ]; then
    CMD_ARGS+=("--api-admin-enabled")
fi

if [ "${INDEX_ENABLED:-true}" = "true" ]; then
    CMD_ARGS+=("--index-enabled")
fi

# Start the node in background
luxd "${CMD_ARGS[@]}" > /luxd/import.log 2>&1 &
NODE_PID=$!

echo "Node restarted with PID: $NODE_PID"

# Step 6: Monitor import progress
echo ""
echo "Monitoring import progress..."

# Function to check if import is complete
check_import_complete() {
    # Check if the import line appears in logs
    if grep -q "successfully imported chain data" "/luxd/import.log" 2>/dev/null; then
        return 0
    fi
    
    # Also check if we can query the block height
    RESPONSE=$(curl -s -X POST http://localhost:9650/ext/bc/C/rpc \
        -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' 2>/dev/null || echo "{}")
    
    if echo "$RESPONSE" | grep -q "result" && [ "$(echo "$RESPONSE" | jq -r '.result' 2>/dev/null)" != "0x0" ]; then
        return 0
    fi
    
    return 1
}

# Wait for import to complete (max 10 minutes)
TIMEOUT=600
ELAPSED=0
while [ $ELAPSED -lt $TIMEOUT ]; do
    if check_import_complete; then
        echo ""
        echo "✅ Import completed successfully!"
        
        # Check final block height
        RESPONSE=$(curl -s -X POST http://localhost:9650/ext/bc/C/rpc \
            -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}')
        
        BLOCK_HEIGHT=$(echo "$RESPONSE" | jq -r '.result' 2>/dev/null || echo "0x0")
        BLOCK_HEIGHT_DEC=$((16#${BLOCK_HEIGHT#0x}))
        echo "Current block height: $BLOCK_HEIGHT_DEC"
        
        # Check balance
        BALANCE_RESPONSE=$(curl -s -X POST http://localhost:9650/ext/bc/C/rpc \
            -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x9011E888251AB053B7bD1cdB598Db4f9DEd94714","latest"],"id":1}')
        
        BALANCE=$(echo "$BALANCE_RESPONSE" | jq -r '.result' 2>/dev/null || echo "0x0")
        echo "Balance of critical address: $BALANCE (should be 0x193e5939a08ce9dbd480000000 = 2T LUX)"
        
        # Update config to remove import directive and enable automining
        echo ""
        echo "Updating config for normal operation..."
        cat > "$CONFIG_FILE" <<EOF
{
  "snowman-api-enabled": false,
  "coreth-admin-api-enabled": true,
  "eth-apis": ["eth", "eth-filter", "net", "web3", "internal-eth", "internal-blockchain", "internal-debug", "internal-tx-pool", "debug", "trace"],
  "personal-api-enabled": true,
  "database-backend": "pebbledb",
  "enable-automining": true,
  "automining-interval": "2s",
  "local-txs-enabled": true,
  "api-max-duration": 0,
  "api-max-blocks-per-request": 0,
  "api-max-gas-per-request": 0,
  "ws-enabled": true,
  "ws-port": 8546,
  "allow-unfinalized-queries": true,
  "log-level": "info",
  "state-sync-enabled": false,
  "pruning-enabled": false,
  "tx-pool-price-limit": 1,
  "tx-pool-account-slots": 16,
  "tx-pool-global-slots": 4096,
  "tx-pool-account-queue": 64,
  "tx-pool-global-queue": 1024,
  "rpc-gas-cap": 0,
  "rpc-tx-fee-cap": 0
}
EOF
        
        # Mark import as done
        rm -f /luxd/.import_pending
        touch /luxd/.import_done
        
        echo ""
        echo "✅ Import complete! The node will continue running normally."
        echo ""
        echo "Chain data has been successfully imported from subnet-evm to C-Chain."
        echo "The network upgrade is complete!"
        
        # Node continues running
        wait $NODE_PID
        exit 0
    fi
    
    # Show progress
    if [ $((ELAPSED % 10)) -eq 0 ]; then
        echo -n "."
    fi
    
    sleep 1
    ELAPSED=$((ELAPSED + 1))
done

echo ""
echo "❌ Import timed out after $TIMEOUT seconds"
echo "Check the logs at: /luxd/import.log"
echo "Node PID: $NODE_PID"
exit 1