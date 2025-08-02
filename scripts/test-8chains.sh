#!/bin/bash
# Test script for 8-chain Lux network

set -e

echo "🚀 Testing 8-Chain Lux Network Configuration"
echo "============================================"

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test configuration
CONFIG_FILE="test-8chains-config.json"
DATA_DIR="./test-8chains-data"
LOG_FILE="./test-8chains.log"

# Clean up previous test data
echo -e "${YELLOW}🧹 Cleaning up previous test data...${NC}"
rm -rf "$DATA_DIR"
rm -f "$LOG_FILE"

# Create necessary directories
echo -e "${YELLOW}📁 Creating test directories...${NC}"
mkdir -p "$DATA_DIR"
mkdir -p "$DATA_DIR/chains"

# Generate chain configurations
echo -e "${YELLOW}⚙️  Generating chain configurations...${NC}"
cat > "$DATA_DIR/chains/A.json" << EOF
{
  "vm-id": "juFxSrbCM4wszxddKepj1GWwmrn9YgN1g4n3VUWPpRo9JjERA",
  "vm-type": "AIVM",
  "config": {
    "blockTime": 2000,
    "minGasPrice": 1000000000
  }
}
EOF

cat > "$DATA_DIR/chains/B.json" << EOF
{
  "vm-id": "kMhHABHM8j4bH94MCc4rsTNdo5E9En37MMyiujk4WdNxgXFsY",
  "vm-type": "BridgeVM",
  "config": {
    "blockTime": 3000,
    "minSignatures": 5
  }
}
EOF

cat > "$DATA_DIR/chains/M.json" << EOF
{
  "vm-id": "qCURact1n41FcoNBch8iMVBwc9AWie48D118ZNJ5tBdWrvryS",
  "vm-type": "MPCVM",
  "config": {
    "blockTime": 2000,
    "mpcThreshold": 5
  }
}
EOF

cat > "$DATA_DIR/chains/Q.json" << EOF
{
  "vm-id": "ry9Sg8rZdT26iEKvJDmC2wkESs4SDKgZEhk5BgLSwg1EpcNug",
  "vm-type": "QuantumVM",
  "config": {
    "blockTime": 2000,
    "signatureAlgo": "sphincs+"
  }
}
EOF

cat > "$DATA_DIR/chains/Z.json" << EOF
{
  "vm-id": "vv3qPfyTVXZ5ArRZA9Jh4hbYDTBe43f7sgQg4CHfNg1rnnvX9",
  "vm-type": "ZKVM",
  "config": {
    "blockTime": 4000,
    "proofSystem": "plonk"
  }
}
EOF

# Build the node if needed
if [ ! -f "./bin/luxd" ]; then
    echo -e "${YELLOW}🔨 Building luxd...${NC}"
    make
fi

# Start the node with all 8 chains
echo -e "${GREEN}🚀 Starting Lux node with all 8 chains...${NC}"
echo -e "${YELLOW}   P-Chain (Platform)${NC}"
echo -e "${YELLOW}   C-Chain (EVM)${NC}"
echo -e "${YELLOW}   X-Chain (Exchange)${NC}"
echo -e "${YELLOW}   A-Chain (AI)${NC}"
echo -e "${YELLOW}   B-Chain (Bridge)${NC}"
echo -e "${YELLOW}   M-Chain (MPC)${NC}"
echo -e "${YELLOW}   Q-Chain (Quantum)${NC}"
echo -e "${YELLOW}   Z-Chain (ZK)${NC}"

# Run the node in background
./bin/luxd \
    --config-file="$CONFIG_FILE" \
    --data-dir="$DATA_DIR" \
    --chain-config-dir="$DATA_DIR/chains" \
    --log-level=info \
    > "$LOG_FILE" 2>&1 &

NODE_PID=$!
echo -e "${GREEN}✓ Node started with PID: $NODE_PID${NC}"

# Wait for node to start
echo -e "${YELLOW}⏳ Waiting for node to initialize...${NC}"
sleep 10

# Check if node is running
if ! kill -0 $NODE_PID 2>/dev/null; then
    echo -e "${RED}❌ Node failed to start! Check $LOG_FILE for details.${NC}"
    tail -20 "$LOG_FILE"
    exit 1
fi

# Test RPC endpoints
echo -e "${YELLOW}🔍 Testing RPC endpoints...${NC}"

# Function to test RPC endpoint
test_rpc() {
    local chain=$1
    local endpoint=$2
    local method=$3
    
    echo -n "   Testing $chain-Chain: "
    
    response=$(curl -s -X POST --data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"$method\",\"params\":[]}" \
        -H 'content-type:application/json;' \
        http://127.0.0.1:9650$endpoint 2>/dev/null || echo "FAILED")
    
    if [[ "$response" == *"result"* ]]; then
        echo -e "${GREEN}✓${NC}"
    else
        echo -e "${RED}✗${NC}"
        echo "     Response: $response"
    fi
}

# Test standard chains
test_rpc "P" "/ext/bc/P" "platform.getHeight"
test_rpc "C" "/ext/bc/C/rpc" "eth_blockNumber"
test_rpc "X" "/ext/bc/X" "avm.getHeight"

# Test custom chains (these would need actual implementation)
echo -e "${YELLOW}   Custom chains would be tested once VMs are implemented${NC}"

# Check node health
echo -e "${YELLOW}🏥 Checking node health...${NC}"
health=$(curl -s http://127.0.0.1:9650/ext/health | jq -r '.healthy' 2>/dev/null || echo "false")

if [ "$health" == "true" ]; then
    echo -e "${GREEN}✓ Node is healthy${NC}"
else
    echo -e "${RED}✗ Node health check failed${NC}"
fi

# Display logs
echo -e "${YELLOW}📋 Recent logs:${NC}"
tail -20 "$LOG_FILE"

# Cleanup
echo -e "${YELLOW}🛑 Stopping node...${NC}"
kill $NODE_PID 2>/dev/null || true
wait $NODE_PID 2>/dev/null || true

echo -e "${GREEN}✅ Test completed!${NC}"
echo -e "${YELLOW}   Check $LOG_FILE for full details${NC}"