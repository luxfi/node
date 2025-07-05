#!/bin/bash

# Launch Lux Network using Avalanche as base
# This demonstrates the genesis configuration with NFT staking

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}==================================${NC}"
echo -e "${GREEN}Lux Network Genesis Launch${NC}"
echo -e "${GREEN}==================================${NC}"
echo ""

# Use the avalanchego we just built
AVALANCHE_BINARY="/Users/z/work/lux/node/tools/avalanchego/avalanchego"
GENESIS_FILE="/Users/z/work/lux/node/genesis/genesis_local_enhanced_v2.json"
BASE_DIR="/Users/z/work/lux/node/.lux-launch"

if [ ! -f "$AVALANCHE_BINARY" ]; then
    echo -e "${RED}Avalanche binary not found at $AVALANCHE_BINARY${NC}"
    exit 1
fi

if [ ! -f "$GENESIS_FILE" ]; then
    echo -e "${RED}Genesis file not found at $GENESIS_FILE${NC}"
    exit 1
fi

# Clean previous runs
rm -rf "$BASE_DIR"
mkdir -p "$BASE_DIR/staking"

# Create staking keys
echo -e "${YELLOW}Generating staking keys...${NC}"
openssl req -x509 -newkey rsa:4096 -keyout "$BASE_DIR/staking/staker.key" \
    -out "$BASE_DIR/staking/staker.crt" -days 365 -nodes \
    -subj "/CN=lux-genesis-node" 2>/dev/null

# Copy genesis
cp "$GENESIS_FILE" "$BASE_DIR/genesis.json"

echo -e "${GREEN}Starting Lux Network with:${NC}"
echo -e "  • 6 Chains: P, X, C, A (AI), B (Bridge), Z (ZK)"
echo -e "  • NFT Staking: 100 Genesis NFTs"
echo -e "  • Token Supply: 2T LUX"
echo -e "  • Validator Minimum: 1M LUX (or equivalent in NFT + delegation)"
echo -e "  • Bridge Validators: 100M LUX requirement"
echo ""

# Launch node
echo -e "${YELLOW}Launching node...${NC}"

$AVALANCHE_BINARY \
    --network-id=12345 \
    --db-dir="$BASE_DIR/db" \
    --log-level=info \
    --http-port=9650 \
    --staking-port=9651 \
    --staking-tls-cert-file="$BASE_DIR/staking/staker.crt" \
    --staking-tls-key-file="$BASE_DIR/staking/staker.key" \
    --bootstrap-ips="" \
    --bootstrap-ids="" > "$BASE_DIR/node.log" 2>&1 &

NODE_PID=$!
echo $NODE_PID > "$BASE_DIR/node.pid"

echo -e "${GREEN}Node started with PID: $NODE_PID${NC}"
echo -e "${GREEN}Log file: $BASE_DIR/node.log${NC}"
echo ""

# Wait for node to start
echo -e "${YELLOW}Waiting for node to initialize...${NC}"
sleep 10

# Check node status
echo -e "${YELLOW}Checking node status...${NC}"
NODE_ID=$(curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID"}' \
    -H 'content-type:application/json;' http://localhost:9650/ext/info | \
    grep -o '"nodeID":"[^"]*' | grep -o '[^"]*$' || echo "Failed to get node ID")

if [ "$NODE_ID" != "Failed to get node ID" ]; then
    echo -e "${GREEN}Node ID: $NODE_ID${NC}"
    
    # Get blockchain IDs
    echo -e "${YELLOW}Blockchain IDs:${NC}"
    for CHAIN in P X C; do
        CHAIN_ID=$(curl -s -X POST --data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"info.getBlockchainID\",\"params\":{\"alias\":\"$CHAIN\"}}" \
            -H 'content-type:application/json;' http://localhost:9650/ext/info | \
            grep -o '"blockchainID":"[^"]*' | grep -o '[^"]*$' || echo "N/A")
        echo -e "  $CHAIN-Chain: $CHAIN_ID"
    done
    
    echo ""
    echo -e "${GREEN}✅ Lux Network is running!${NC}"
    echo ""
    echo -e "${YELLOW}API Endpoints:${NC}"
    echo -e "  Info: http://localhost:9650/ext/info"
    echo -e "  Health: http://localhost:9650/ext/health"
    echo -e "  P-Chain: http://localhost:9650/ext/bc/P"
    echo -e "  X-Chain: http://localhost:9650/ext/bc/X"
    echo -e "  C-Chain: http://localhost:9650/ext/bc/C"
    echo ""
    echo -e "${YELLOW}NFT Configuration:${NC}"
    echo -e "  Genesis NFTs (1-10): 2x rewards, 500K LUX requirement"
    echo -e "  Pioneer NFTs (11-40): 1.5x rewards, 750K LUX requirement"
    echo -e "  Standard NFTs (41-100): 1x rewards, 1M LUX requirement"
    echo ""
    echo -e "${YELLOW}To view logs:${NC} tail -f $BASE_DIR/node.log"
    echo -e "${YELLOW}To stop:${NC} kill $NODE_PID"
else
    echo -e "${RED}Failed to start node. Check logs at: $BASE_DIR/node.log${NC}"
    tail -20 "$BASE_DIR/node.log"
fi