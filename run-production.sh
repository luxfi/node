#!/bin/bash
# Production LUX node runner
# Single-node C-Chain for development
# Network ID: 96369

set -e

NODE_DIR="/home/z/work/lux/node"
DATA_DIR="/home/z/work/lux/.luxd-production"
LOG_FILE="/var/log/luxd.log"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

# Check if running as root (for systemd)
if [ "$EUID" -eq 0 ]; then
   echo -e "${RED}Warning: Running as root. Consider using a dedicated user.${NC}"
fi

# Parse arguments
case "$1" in
    start)
        echo -e "${GREEN}Starting LUX node...${NC}"
        if pgrep luxd > /dev/null; then
            echo "LUX node is already running"
            exit 1
        fi

        mkdir -p "$DATA_DIR"
        nohup "$NODE_DIR/build/luxd" \
            --network-id=96369 \
            --http-host=127.0.0.1 \
            --http-port=9630 \
            --staking-port=9631 \
            --db-dir="$DATA_DIR" \
            --dev \
            --log-level=info > "$LOG_FILE" 2>&1 &

        echo "LUX node started (PID: $!)"
        echo "C-Chain RPC: http://127.0.0.1:9630/ext/bc/C/rpc"
        ;;

    stop)
        echo -e "${GREEN}Stopping LUX node...${NC}"
        pkill luxd || echo "LUX node not running"
        ;;

    status)
        if pgrep luxd > /dev/null; then
            echo -e "${GREEN}LUX node is running${NC}"
            curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}' \
                 -H 'content-type:application/json' http://127.0.0.1:9630/ext/bc/C/rpc | \
                 jq -r '"Current block: " + (.result | tonumber | tostring)'
        else
            echo -e "${RED}LUX node is not running${NC}"
            exit 1
        fi
        ;;

    logs)
        tail -f "$LOG_FILE"
        ;;

    clean)
        echo -e "${RED}Cleaning all data...${NC}"
        read -p "This will delete all blockchain data. Continue? (y/N) " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            pkill luxd 2>/dev/null || true
            rm -rf "$DATA_DIR"
            echo "Data cleaned"
        fi
        ;;

    *)
        echo "Usage: $0 {start|stop|status|logs|clean}"
        exit 1
        ;;
esac