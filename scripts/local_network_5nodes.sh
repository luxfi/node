#!/bin/bash

# Local network setup script for 5 Lux nodes with NFT staking
# This script sets up a local test network with 5 validator nodes

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Setting up Lux Network with 5 validator nodes...${NC}"

# Configuration
NETWORK_ID=12345
API_PORT_BASE=9650
STAKING_PORT_BASE=9651
HTTP_HOST="127.0.0.1"

# Node directories
BASE_DIR="$HOME/.lux-local-network"
NODES_DIR="$BASE_DIR/nodes"

# Clean up existing network
if [ -d "$BASE_DIR" ]; then
    echo -e "${YELLOW}Cleaning up existing network...${NC}"
    rm -rf "$BASE_DIR"
fi

# Create directories
mkdir -p "$BASE_DIR"
mkdir -p "$NODES_DIR"

# Copy genesis file
cp genesis/genesis_local_enhanced.json "$BASE_DIR/genesis.json"

# Node configurations
declare -a NODE_IDS=(
    "NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg"
    "NodeID-MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ"
    "NodeID-NFBbbJ4qCmNaCzeW7sxErhvWqvEQMnYcN"
    "NodeID-GWPcbFJZFfZreETSoWjPimr846mXEKCtu"
    "NodeID-P7oB2McjBGgW2NXXWVYjV8JEDFoW9xDE5"
)

# Staking keys (these should match the keys in genesis)
declare -a STAKING_KEYS=(
    "PrivateKey-ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN"
    "PrivateKey-2uVvTVjmDGVJGKk5C4eLpX5Zf9KDYKSVauw3YwJQoFUpFg8fE"
    "PrivateKey-vmRQiZeXEXYMyJhEiqdC2z5JhuDbxL8ix9UVvjgMu2Er1NepE"
    "PrivateKey-2kjG6Wn9Zr8s5cssvWmB2ZPpZS4EqL3VVNsL8XYbGc5qcUMjK"
    "PrivateKey-2Y2rGLwCzsFQFJbMksL9F8nrPRGCcGNVoRCCKRpBT8K2kzc7v"
)

# Start each node
for i in {0..4}; do
    NODE_NAME="node$((i+1))"
    NODE_DIR="$NODES_DIR/$NODE_NAME"
    mkdir -p "$NODE_DIR"
    
    API_PORT=$((API_PORT_BASE + i))
    STAKING_PORT=$((STAKING_PORT_BASE + i))
    
    echo -e "${GREEN}Starting $NODE_NAME...${NC}"
    echo "  Node ID: ${NODE_IDS[$i]}"
    echo "  API Port: $API_PORT"
    echo "  Staking Port: $STAKING_PORT"
    
    # Generate node config
    cat > "$NODE_DIR/config.json" <<EOF
{
    "network-id": $NETWORK_ID,
    "http-host": "$HTTP_HOST",
    "http-port": $API_PORT,
    "staking-port": $STAKING_PORT,
    "db-dir": "$NODE_DIR/db",
    "log-dir": "$NODE_DIR/logs",
    "log-level": "info",
    "snow-sample-size": 5,
    "snow-quorum-size": 3,
    "staking-enabled": true,
    "staking-tls-cert-file": "$NODE_DIR/staker.crt",
    "staking-tls-key-file": "$NODE_DIR/staker.key",
    "genesis-file": "$BASE_DIR/genesis.json",
    "bootstrap-ips": "",
    "bootstrap-ids": "",
    "nft-staking-enabled": true,
    "ringtail-signatures-enabled": true,
    "per-account-mpc-enabled": true
}
EOF

    # Copy staking certificates (in production, these would be generated)
    # For now, we'll generate placeholder certificates
    openssl req -x509 -newkey rsa:4096 -keyout "$NODE_DIR/staker.key" -out "$NODE_DIR/staker.crt" -days 365 -nodes -subj "/CN=${NODE_IDS[$i]}" 2>/dev/null
    
    # Set bootstrap nodes (all nodes except the current one)
    if [ $i -gt 0 ]; then
        BOOTSTRAP_IPS=""
        BOOTSTRAP_IDS=""
        for j in $(seq 0 $((i-1))); do
            if [ -n "$BOOTSTRAP_IPS" ]; then
                BOOTSTRAP_IPS="$BOOTSTRAP_IPS,"
                BOOTSTRAP_IDS="$BOOTSTRAP_IDS,"
            fi
            BOOTSTRAP_IPS="${BOOTSTRAP_IPS}127.0.0.1:$((STAKING_PORT_BASE + j))"
            BOOTSTRAP_IDS="${BOOTSTRAP_IDS}${NODE_IDS[$j]}"
        done
        
        # Update config with bootstrap info
        jq --arg ips "$BOOTSTRAP_IPS" --arg ids "$BOOTSTRAP_IDS" \
           '.["bootstrap-ips"] = $ips | .["bootstrap-ids"] = $ids' \
           "$NODE_DIR/config.json" > "$NODE_DIR/config.tmp" && \
           mv "$NODE_DIR/config.tmp" "$NODE_DIR/config.json"
    fi
    
    # Start the node
    nohup ./build/node --config-file="$NODE_DIR/config.json" > "$NODE_DIR/node.log" 2>&1 &
    NODE_PID=$!
    echo $NODE_PID > "$NODE_DIR/node.pid"
    
    echo -e "${GREEN}$NODE_NAME started with PID $NODE_PID${NC}"
    
    # Wait a bit before starting the next node
    sleep 2
done

echo -e "${GREEN}All nodes started!${NC}"
echo
echo "Node endpoints:"
for i in {0..4}; do
    API_PORT=$((API_PORT_BASE + i))
    echo "  Node $((i+1)): http://127.0.0.1:$API_PORT"
done

echo
echo "To check node status:"
echo "  curl -X POST --data '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"info.isBootstrapped\",\"params\":{}}' -H 'content-type:application/json;' http://127.0.0.1:9650/ext/info"

echo
echo "To stop the network:"
echo "  $BASE_DIR/../scripts/stop_local_network.sh"