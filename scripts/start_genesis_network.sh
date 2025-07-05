#!/bin/bash

# Start Lux Network with Genesis NFT configuration
# This script starts a local 5-node network with NFT staking, Ringtail signatures, and MPC

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Starting Lux Network Genesis Configuration${NC}"
echo -e "${YELLOW}Features: NFT Staking, Ringtail Signatures, Per-Account MPC, GPU Mining${NC}"
echo ""

# Configuration
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
BASE_DIR="$HOME/.lux-genesis-network"
NODES_DIR="$BASE_DIR/nodes"
GENESIS_FILE="$SCRIPT_DIR/../genesis/genesis_local_enhanced_v2.json"
NODE_BINARY="$SCRIPT_DIR/../build/luxd"

# Check if node binary exists
if [ ! -f "$NODE_BINARY" ]; then
    echo -e "${RED}Error: Node binary not found at $NODE_BINARY${NC}"
    echo "Please build the node first with: go build -o build/luxd ./node"
    exit 1
fi

# Check if genesis file exists
if [ ! -f "$GENESIS_FILE" ]; then
    echo -e "${RED}Error: Genesis file not found at $GENESIS_FILE${NC}"
    exit 1
fi

# Create base directories
echo -e "${YELLOW}Creating network directories...${NC}"
mkdir -p "$NODES_DIR"

# Node configurations with NFT assignments
NODES=(
    "node1:9650:9651:Genesis:1"
    "node2:9652:9653:Genesis:2"
    "node3:9654:9655:Pioneer:11"
    "node4:9656:9657:Standard:41"
    "node5:9658:9659:Standard:42"
)

# Start each node
for NODE_CONFIG in "${NODES[@]}"; do
    IFS=':' read -r NODE_NAME HTTP_PORT STAKING_PORT NFT_TIER TOKEN_ID <<< "$NODE_CONFIG"
    NODE_DIR="$NODES_DIR/$NODE_NAME"
    
    echo -e "${YELLOW}Configuring $NODE_NAME (NFT Tier: $NFT_TIER, Token ID: $TOKEN_ID)...${NC}"
    
    # Create node directory
    mkdir -p "$NODE_DIR"
    
    # Copy genesis file
    cp "$GENESIS_FILE" "$NODE_DIR/genesis.json"
    
    # Create node configuration
    cat > "$NODE_DIR/config.json" <<EOF
{
    "http-port": $HTTP_PORT,
    "staking-port": $STAKING_PORT,
    "db-dir": "$NODE_DIR/db",
    "log-dir": "$NODE_DIR/logs",
    "log-level": "info",
    "network-id": "local",
    "staking-enabled": true,
    "nft-staking": {
        "enabled": true,
        "tokenId": $TOKEN_ID,
        "tier": "$NFT_TIER"
    },
    "ringtail": {
        "enabled": true,
        "ringSize": 16
    },
    "mpc": {
        "enabled": true,
        "threshold": 3,
        "parties": 5
    },
    "chains": {
        "A": {
            "gpu-subnet": {
                "enabled": true,
                "provider": $([ "$NODE_NAME" = "node1" ] || [ "$NODE_NAME" = "node2" ] && echo "true" || echo "false"),
                "gpu-count": 2,
                "gpu-type": "RTX 4090"
            }
        },
        "B": {
            "bridge-validator": $([ "$NFT_TIER" = "Genesis" ] && echo "true" || echo "false"),
            "minimum-stake": $([ "$NFT_TIER" = "Genesis" ] && echo "100000000000000000" || echo "1000000000000000")
        }
    }
}
EOF

    # Create staking key if not exists
    if [ ! -f "$NODE_DIR/staking.key" ]; then
        echo -e "${YELLOW}Generating staking key for $NODE_NAME...${NC}"
        # In production, use proper key generation
        openssl genpkey -algorithm RSA -out "$NODE_DIR/staking.key" -pkeyopt rsa_keygen_bits:4096 2>/dev/null
        openssl rsa -in "$NODE_DIR/staking.key" -pubout -out "$NODE_DIR/staking.crt" 2>/dev/null
    fi
    
    # Start the node
    echo -e "${GREEN}Starting $NODE_NAME...${NC}"
    nohup "$NODE_BINARY" \
        --config-file="$NODE_DIR/config.json" \
        --genesis="$NODE_DIR/genesis.json" \
        > "$NODE_DIR/node.log" 2>&1 &
    
    NODE_PID=$!
    echo $NODE_PID > "$NODE_DIR/node.pid"
    
    echo -e "${GREEN}$NODE_NAME started with PID $NODE_PID${NC}"
    echo -e "  HTTP API: http://localhost:$HTTP_PORT"
    echo -e "  Staking Port: $STAKING_PORT"
    echo -e "  NFT: $NFT_TIER #$TOKEN_ID"
    echo ""
    
    # Give node time to start
    sleep 2
done

# Wait for nodes to initialize
echo -e "${YELLOW}Waiting for nodes to initialize...${NC}"
sleep 10

# Display network information
echo -e "${GREEN}================================${NC}"
echo -e "${GREEN}Lux Genesis Network Started!${NC}"
echo -e "${GREEN}================================${NC}"
echo ""
echo -e "${YELLOW}Network Features:${NC}"
echo -e "  • 6 Chains: P, X, C, A (AI), B (Bridge), Z (ZK)"
echo -e "  • NFT Staking: 100 Genesis NFTs distributed"
echo -e "  • Ringtail Signatures: Privacy-preserving validation"
echo -e "  • Per-Account MPC: Distributed key management"
echo -e "  • GPU Mining: NFT-gated mining on A-Chain"
echo -e "  • Multi-Consensus: Support for OP Stack subnets"
echo ""
echo -e "${YELLOW}Token Economics:${NC}"
echo -e "  • Total Supply: 2T LUX"
echo -e "  • Validator Minimum: 1M LUX (combined staking allowed)"
echo -e "  • Bridge Validator: 100M LUX (B-Chain only)"
echo -e "  • REQL Airdrop: 1:1000 conversion ratio"
echo ""
echo -e "${YELLOW}Node Endpoints:${NC}"
for NODE_CONFIG in "${NODES[@]}"; do
    IFS=':' read -r NODE_NAME HTTP_PORT STAKING_PORT NFT_TIER TOKEN_ID <<< "$NODE_CONFIG"
    echo -e "  $NODE_NAME: http://localhost:$HTTP_PORT (NFT: $NFT_TIER #$TOKEN_ID)"
done
echo ""
echo -e "${YELLOW}NFT Distribution:${NC}"
echo -e "  • Genesis (1-10): 2x rewards, 500K LUX requirement"
echo -e "  • Pioneer (11-40): 1.5x rewards, 750K LUX requirement"
echo -e "  • Standard (41-100): 1x rewards, 1M LUX requirement"
echo ""
echo -e "${GREEN}To stop the network, run: ./scripts/stop_local_network.sh${NC}"
echo -e "${GREEN}To check status: curl -X POST --data '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"info.getNodeID\"}' -H 'content-type:application/json;' http://localhost:9650/ext/info${NC}"

# Create status checking script
cat > "$BASE_DIR/check_status.sh" <<'EOF'
#!/bin/bash
echo "Checking node status..."
for port in 9650 9652 9654 9656 9658; do
    echo -n "Node on port $port: "
    curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID"}' \
        -H 'content-type:application/json;' http://localhost:$port/ext/info | \
        jq -r '.result.nodeID' 2>/dev/null || echo "Not responding"
done
EOF
chmod +x "$BASE_DIR/check_status.sh"

echo ""
echo -e "${GREEN}Status check script created at: $BASE_DIR/check_status.sh${NC}"