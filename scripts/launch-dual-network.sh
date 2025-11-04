#!/bin/bash
# Launch Lux Mainnet and Testnet validation in parallel
# Nets (Zoo, Hanzo, SPC) are L1s/L2s on these primary networks

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
LUXD_BIN="${LUXD_BIN:-/home/z/work/lux/node/build/luxd}"
BASE_DATA_DIR="${BASE_DATA_DIR:-/home/z/.luxd-dual}"

# Function to print colored output
print_color() {
    local color=$1
    local message=$2
    echo -e "${color}${message}${NC}"
}

# Function to launch a network
launch_network() {
    local network_name=$1
    local network_id=$2
    local http_port=$3
    local staking_port=$4

    # Network-specific data directory
    local data_dir="$BASE_DATA_DIR/network-$network_id"

    print_color "$BLUE" "🔷 Launching $network_name (Network ID: $network_id)"

    # Create directories
    mkdir -p "$data_dir"/{staking,db,logs,chainData,plugins}

    # Generate staking keys if they don't exist
    if [ ! -f "$data_dir/staking/staker.key" ]; then
        openssl genrsa -out "$data_dir/staking/staker.key" 4096 2>/dev/null
        openssl req -x509 -new -nodes \
            -key "$data_dir/staking/staker.key" \
            -sha256 -days 3650 \
            -out "$data_dir/staking/staker.crt" \
            -subj "/CN=$network_name.lux.network" 2>/dev/null
    fi

    # Launch the node
    $LUXD_BIN \
        --network-id=$network_id \
        --data-dir="$data_dir" \
        --db-dir="$data_dir/db" \
        --chain-data-dir="$data_dir/chainData" \
        --http-host=0.0.0.0 \
        --http-port=$http_port \
        --staking-port=$staking_port \
        --public-ip=127.0.0.1 \
        --http-allowed-hosts="*" \
        --consensus-sample-size=1 \
        --consensus-quorum-size=1 \
        --log-level=info \
        --log-dir="$data_dir/logs" \
        > "$data_dir/node.log" 2>&1 &

    local pid=$!
    echo $pid > "$data_dir/pid"

    print_color "$GREEN" "  ✅ Started with PID: $pid"
    print_color "$GREEN" "  📍 HTTP: http://localhost:$http_port"
    print_color "$GREEN" "  📍 Staking: localhost:$staking_port"
    echo ""
}

# Function to check health
check_health() {
    local port=$1
    local network=$2

    if curl -s "http://127.0.0.1:$port/ext/health" > /dev/null 2>&1; then
        local p_chain_height=$(curl -s -X POST -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","method":"platform.getHeight","params":{},"id":1}' \
            "http://127.0.0.1:$port/ext/P" 2>/dev/null | jq -r '.result.height' 2>/dev/null || echo "0")

        local validators=$(curl -s -X POST -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","method":"platform.getCurrentValidators","params":{},"id":1}' \
            "http://127.0.0.1:$port/ext/P" 2>/dev/null | jq -r '.result.validators | length' 2>/dev/null || echo "0")

        print_color "$GREEN" "  $network: ✅ Healthy (Height: $p_chain_height, Validators: $validators)"
    else
        print_color "$RED" "  $network: ❌ Not responding"
    fi
}

# Function to list subnets
list_subnets() {
    local port=$1
    local network=$2

    print_color "$BLUE" "\n  Nets on $network:"

    # Get subnet list from P-Chain
    local subnets=$(curl -s -X POST -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","method":"platform.getNets","params":{},"id":1}' \
        "http://127.0.0.1:$port/ext/P" 2>/dev/null | jq -r '.result.subnets[]?.id' 2>/dev/null)

    if [ -z "$subnets" ]; then
        echo "    No subnets found or P-Chain not ready"
    else
        echo "$subnets" | while read subnet_id; do
            echo "    • Net: $subnet_id"
        done
    fi
}

# Main execution
case "${1:-launch}" in
    launch)
        print_color "$YELLOW" "🚀 Launching Lux Primary Networks (Mainnet & Testnet)"
        print_color "$YELLOW" "===================================================\n"

        # Stop any existing nodes
        print_color "$YELLOW" "⚠️  Stopping any existing nodes..."
        pkill -f luxd || true
        sleep 2

        # Clean and create base directory
        rm -rf "$BASE_DATA_DIR"
        mkdir -p "$BASE_DATA_DIR"

        # Launch Mainnet
        launch_network "Lux Mainnet" 96369 9630 9631

        # Launch Testnet
        launch_network "Lux Testnet" 96368 9620 9621

        print_color "$GREEN" "================================================"
        print_color "$GREEN" "✅ BOTH PRIMARY NETWORKS LAUNCHED!"
        print_color "$GREEN" "================================================\n"

        # Wait for bootstrap
        print_color "$YELLOW" "⏳ Waiting for networks to bootstrap..."
        sleep 10

        # Check health
        print_color "$BLUE" "📊 Network Status:"
        echo "=================="
        check_health 9630 "Lux Mainnet"
        check_health 9620 "Lux Testnet"

        # List subnets (L1s/L2s)
        list_subnets 9630 "Lux Mainnet"
        list_subnets 9620 "Lux Testnet"

        # Display info
        print_color "$BLUE" "\n📈 Access Points:"
        echo "================="
        echo "  Lux Mainnet:"
        echo "    • RPC: http://localhost:9630/ext/bc/C/rpc"
        echo "    • P-Chain: http://localhost:9630/ext/P"
        echo "    • X-Chain: http://localhost:9630/ext/X"
        echo "    • C-Chain: http://localhost:9630/ext/bc/C/rpc"
        echo ""
        echo "  Lux Testnet:"
        echo "    • RPC: http://localhost:9620/ext/bc/C/rpc"
        echo "    • P-Chain: http://localhost:9620/ext/P"
        echo "    • X-Chain: http://localhost:9620/ext/X"
        echo "    • C-Chain: http://localhost:9620/ext/bc/C/rpc"
        echo ""
        print_color "$YELLOW" "📝 Note: Zoo, Hanzo, and SPC are subnets (L1s/L2s) on these primary networks"
        echo ""
        print_color "$BLUE" "🛠️  Management:"
        echo "=============="
        echo "  Status:  $0 status"
        echo "  Stop:    $0 stop"
        echo "  Logs:    tail -f $BASE_DATA_DIR/network-*/node.log"
        ;;

    stop)
        print_color "$YELLOW" "⚠️  Stopping all nodes..."
        pkill -f luxd || true
        sleep 2
        print_color "$GREEN" "✅ All nodes stopped"
        ;;

    status)
        print_color "$BLUE" "📊 Network Status:"
        echo "=================="
        check_health 9630 "Lux Mainnet (96369)"
        check_health 9620 "Lux Testnet (96368)"

        list_subnets 9630 "Lux Mainnet"
        list_subnets 9620 "Lux Testnet"
        ;;

    *)
        echo "Usage: $0 {launch|stop|status}"
        exit 1
        ;;
esac