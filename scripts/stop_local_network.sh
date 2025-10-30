#!/bin/bash

# Stop local network script

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Stopping Lux local network...${NC}"

BASE_DIR="$HOME/.lux-local-network"
NODES_DIR="$BASE_DIR/nodes"

if [ ! -d "$BASE_DIR" ]; then
    echo -e "${RED}No local network found.${NC}"
    exit 1
fi

# Stop all nodes
for NODE_DIR in "$NODES_DIR"/*; do
    if [ -d "$NODE_DIR" ]; then
        NODE_NAME=$(basename "$NODE_DIR")
        PID_FILE="$NODE_DIR/node.pid"
        
        if [ -f "$PID_FILE" ]; then
            PID=$(cat "$PID_FILE")
            if kill -0 "$PID" 2>/dev/null; then
                echo -e "${YELLOW}Stopping $NODE_NAME (PID: $PID)...${NC}"
                kill "$PID"
                rm "$PID_FILE"
            else
                echo -e "${YELLOW}$NODE_NAME already stopped.${NC}"
            fi
        fi
    fi
done

echo -e "${GREEN}All nodes stopped.${NC}"

# Optional: Clean up data
read -p "Do you want to clean up all node data? (y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo -e "${YELLOW}Cleaning up node data...${NC}"
    rm -rf "$BASE_DIR"
    echo -e "${GREEN}Clean up complete.${NC}"
fi