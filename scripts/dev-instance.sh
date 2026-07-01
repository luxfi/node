#!/bin/bash
# dev-instance.sh - Start isolated luxd dev instances for testing
# Usage: ./scripts/dev-instance.sh [name] [http_port] [staking_port]
#
# Examples:
#   ./scripts/dev-instance.sh dex 7545 7546      # Start DEX testing instance
#   ./scripts/dev-instance.sh amm 7645 7646      # Start AMM testing instance
#   ./scripts/dev-instance.sh exchange 7745 7746 # Start Exchange testing instance
#
# To stop: kill $(cat /tmp/lux-$NAME/luxd.pid) or just `pkill -f "data-dir=/tmp/lux-$NAME"`
# To clean: rm -rf /tmp/lux-$NAME

set -e

NAME="${1:-dev}"
HTTP_PORT="${2:-7545}"
STAKING_PORT="${3:-7546}"
DATA_DIR="/tmp/lux-$NAME"
PID_FILE="$DATA_DIR/luxd.pid"
LOG_FILE="$DATA_DIR/luxd.log"

# Find luxd binary
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LUXD="$SCRIPT_DIR/../build/luxd"

if [[ ! -x "$LUXD" ]]; then
    echo "Error: luxd not found at $LUXD"
    echo "Build with: cd $(dirname "$SCRIPT_DIR") && go build -o build/luxd ./main"
    exit 1
fi

# Check if already running
if [[ -f "$PID_FILE" ]]; then
    OLD_PID=$(cat "$PID_FILE")
    if kill -0 "$OLD_PID" 2>/dev/null; then
        echo "Instance '$NAME' already running (PID: $OLD_PID)"
        echo "Stop with: kill $OLD_PID"
        echo "Or clean restart with: rm -rf $DATA_DIR && $0 $NAME $HTTP_PORT $STAKING_PORT"
        exit 1
    fi
fi

# Check port availability
if lsof -i ":$HTTP_PORT" >/dev/null 2>&1; then
    echo "Error: Port $HTTP_PORT is already in use"
    lsof -i ":$HTTP_PORT" | head -3
    exit 1
fi

echo "Starting isolated luxd instance: $NAME"
echo "  Data directory: $DATA_DIR"
echo "  HTTP port:      $HTTP_PORT"
echo "  Staking port:   $STAKING_PORT"
echo ""

# Clean and create data dir
rm -rf "$DATA_DIR"
mkdir -p "$DATA_DIR"

# Start luxd in background
nohup "$LUXD" --dev \
    --data-dir="$DATA_DIR" \
    --http-port="$HTTP_PORT" \
    --staking-port="$STAKING_PORT" \
    > "$LOG_FILE" 2>&1 &

echo $! > "$PID_FILE"

echo "Started luxd with PID $(cat "$PID_FILE")"
echo ""

# Wait for RPC to be ready
echo -n "Waiting for RPC..."
for _ in {1..30}; do
    if curl -s "http://127.0.0.1:$HTTP_PORT/v1/info" >/dev/null 2>&1; then
        echo " ready!"
        break
    fi
    echo -n "."
    sleep 1
done

# Show status
echo ""
echo "=== Instance Ready ==="
echo "  C-Chain RPC:  http://127.0.0.1:$HTTP_PORT/v1/bc/C/rpc"
echo "  X-Chain RPC:  http://127.0.0.1:$HTTP_PORT/v1/bc/X"
echo "  P-Chain RPC:  http://127.0.0.1:$HTTP_PORT/v1/bc/P"
echo "  Info API:     http://127.0.0.1:$HTTP_PORT/v1/info"
echo ""
echo "  Logs:         tail -f $LOG_FILE"
echo "  Stop:         kill $(cat "$PID_FILE")"
echo "  Clean:        rm -rf $DATA_DIR"
