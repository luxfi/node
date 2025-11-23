#!/bin/bash

# Mainnet Replay Script - Load old mainnet (96369) into C-Chain
# This script uses the existing RuntimeReplayManager to replay blockchain data

set -e

echo "=== Lux Mainnet Replay Script ==="
echo "Replaying from old mainnet (96369) to C-Chain"
echo

# Configuration
OLD_MAINNET_DB="$HOME/work/lux/state/chaindata/lux-mainnet-96369/db"
LUXD_DATA_DIR="$HOME/.luxd"
EXPECTED_BLOCKS=1074616
TREASURY_ADDR="0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"
EXPECTED_TREASURY="61500000000" # ~61.5 billion LUX

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if old mainnet database exists
if [ ! -d "$OLD_MAINNET_DB" ]; then
    echo -e "${RED}Error: Old mainnet database not found at: $OLD_MAINNET_DB${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Found old mainnet database at: $OLD_MAINNET_DB${NC}"

# Check database type
detect_db_type() {
    local db_path="$1"
    
    if [ -d "$db_path/badgerdb" ] || [ -f "$db_path/MANIFEST" ]; then
        echo "badger"
    elif [ -f "$db_path/CURRENT" ] && [ -f "$db_path/MANIFEST-000000" ]; then
        echo "leveldb"
    elif [ -d "$db_path/pebble" ] || [ -f "$db_path/CURRENT" ]; then
        echo "pebble"
    else
        echo "unknown"
    fi
}

DB_TYPE=$(detect_db_type "$OLD_MAINNET_DB")
echo -e "${YELLOW}Detected database type: $DB_TYPE${NC}"

# Set up environment for runtime replay
export LUX_REPLAY_DB="$OLD_MAINNET_DB"
export LUX_RUNTIME_REPLAY="true"
export LUX_REPLAY_HEIGHT="$EXPECTED_BLOCKS"
export CHAIN_DATA_DIR="$HOME/work/lux/state/chaindata"

# Create backup of existing chain data
if [ -d "$LUXD_DATA_DIR/chainData/C" ]; then
    BACKUP_DIR="$LUXD_DATA_DIR/chainData/C.backup-$(date +%Y%m%d-%H%M%S)"
    echo -e "${YELLOW}Backing up existing C-Chain data to: $BACKUP_DIR${NC}"
    mv "$LUXD_DATA_DIR/chainData/C" "$BACKUP_DIR"
fi

# Start the node with runtime replay enabled
echo
echo -e "${GREEN}Starting Lux node with runtime replay...${NC}"
echo "Environment variables set:"
echo "  LUX_REPLAY_DB=$LUX_REPLAY_DB"
echo "  LUX_RUNTIME_REPLAY=$LUX_RUNTIME_REPLAY"
echo "  LUX_REPLAY_HEIGHT=$LUX_REPLAY_HEIGHT"
echo

# Build luxd if needed
if [ ! -f "$HOME/work/lux/node/build/luxd" ]; then
    echo -e "${YELLOW}Building luxd...${NC}"
    cd "$HOME/work/lux/node"
    ./scripts/build.sh
fi

# Start luxd with replay configuration
cd "$HOME/work/lux/node"
echo -e "${GREEN}Starting luxd with runtime replay...${NC}"

# Create a config file for replay
cat > /tmp/luxd-replay-config.json <<EOF
{
  "log-level": "debug",
  "log-display-level": "info",
  "network-id": "lux",
  "db-type": "leveldb",
  "chain-config-dir": "$HOME/.luxd/configs/chains",
  "subnetvm-configs": {
    "C": {
      "runtime-replay": true,
      "replay-db-path": "$OLD_MAINNET_DB",
      "expected-height": $EXPECTED_BLOCKS,
      "treasury-address": "$TREASURY_ADDR"
    }
  }
}
EOF

# Start the node
./build/luxd --config-file=/tmp/luxd-replay-config.json &
LUXD_PID=$!

echo "Luxd started with PID: $LUXD_PID"
echo

# Function to check node status
check_status() {
    curl -s -X POST --data '{
        "jsonrpc":"2.0",
        "id"     :1,
        "method" :"info.isBootstrapped",
        "params": {
            "chain":"C"
        }
    }' -H 'content-type:application/json;' http://127.0.0.1:9650/ext/info | python3 -m json.tool
}

# Function to get block height
get_block_height() {
    local result=$(curl -s -X POST --data '{
        "jsonrpc":"2.0",
        "id"     :1,
        "method" :"eth_blockNumber"
    }' -H 'content-type:application/json;' http://127.0.0.1:9650/ext/bc/C/rpc)
    
    echo "$result" | python3 -c "
import sys, json
data = json.load(sys.stdin)
if 'result' in data:
    print(int(data['result'], 16))
else:
    print(0)
"
}

# Function to get treasury balance
get_treasury_balance() {
    local result=$(curl -s -X POST --data "{
        \"jsonrpc\":\"2.0\",
        \"id\"     :1,
        \"method\" :\"eth_getBalance\",
        \"params\": [\"$TREASURY_ADDR\", \"latest\"]
    }" -H 'content-type:application/json;' http://127.0.0.1:9650/ext/bc/C/rpc)
    
    echo "$result" | python3 -c "
import sys, json
data = json.load(sys.stdin)
if 'result' in data:
    wei = int(data['result'], 16)
    lux = wei / 10**18
    print(f'{lux:,.2f}')
else:
    print('0')
"
}

# Wait for node to start and replay
echo "Waiting for node to bootstrap and replay..."
sleep 10

# Monitor replay progress
RETRY_COUNT=0
MAX_RETRIES=120  # 10 minutes

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    # Check if C-Chain is bootstrapped
    IS_BOOTSTRAPPED=$(check_status 2>/dev/null | grep -o '"result":[^,}]*' | cut -d: -f2 || echo "false")
    
    if [ "$IS_BOOTSTRAPPED" = "true" ]; then
        echo -e "${GREEN}✓ C-Chain bootstrapped${NC}"
        
        # Get current block height
        CURRENT_HEIGHT=$(get_block_height)
        echo "Current block height: $CURRENT_HEIGHT / $EXPECTED_BLOCKS"
        
        if [ "$CURRENT_HEIGHT" -ge "$EXPECTED_BLOCKS" ]; then
            echo -e "${GREEN}✓ Replay complete! Block height: $CURRENT_HEIGHT${NC}"
            
            # Check treasury balance
            TREASURY_BALANCE=$(get_treasury_balance)
            echo -e "${GREEN}Treasury balance: $TREASURY_BALANCE LUX${NC}"
            
            break
        fi
    else
        echo "Waiting for bootstrap... (attempt $((RETRY_COUNT+1))/$MAX_RETRIES)"
    fi
    
    sleep 5
    RETRY_COUNT=$((RETRY_COUNT+1))
done

if [ $RETRY_COUNT -eq $MAX_RETRIES ]; then
    echo -e "${RED}Timeout waiting for replay to complete${NC}"
    kill $LUXD_PID 2>/dev/null
    exit 1
fi

echo
echo -e "${GREEN}=== Replay Summary ===${NC}"
echo "Blocks replayed: $(get_block_height)"
echo "Treasury balance: $(get_treasury_balance) LUX"
echo "Node PID: $LUXD_PID"
echo
echo "To stop the node: kill $LUXD_PID"
echo "To check status: curl -X POST --data '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_blockNumber\"}' -H 'content-type:application/json;' http://127.0.0.1:9650/ext/bc/C/rpc"