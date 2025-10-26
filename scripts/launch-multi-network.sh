#!/bin/bash
# Launch multiple networks with separate data directories for parallel validation
# This allows a single node operator to validate multiple networks simultaneously

set -e

# Network configurations
declare -A NETWORKS
NETWORKS[mainnet]="96369:9630"
NETWORKS[testnet]="96368:9620"
NETWORKS[zoo]="200200:2000"
NETWORKS[hanzo]="36963:3690"

# Base paths
LUXD_BIN="${LUXD_BIN:-/home/z/work/lux/node/build/luxd}"
BASE_DATA_DIR="${BASE_DATA_DIR:-/home/z/.luxd-multinetwork}"

# Number of validators per network
VALIDATORS_PER_NETWORK=${VALIDATORS_PER_NETWORK:-5}

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print colored output
print_color() {
    local color=$1
    local message=$2
    echo -e "${color}${message}${NC}"
}

# Function to launch a single validator
launch_validator() {
    local network_name=$1
    local network_id=$2
    local base_port=$3
    local node_num=$4

    # Calculate ports for this node
    local http_port=$((base_port + (node_num-1)*2))
    local staking_port=$((base_port + (node_num-1)*2 + 1))

    # Network-specific data directory (includes network ID to prevent conflicts)
    local data_dir="$BASE_DATA_DIR/network-$network_id/node$node_num"

    # Create directories
    mkdir -p "$data_dir"/{staking,db,logs,chainData,plugins}

    # Generate staking keys if they don't exist
    if [ ! -f "$data_dir/staking/staker.key" ]; then
        openssl genrsa -out "$data_dir/staking/staker.key" 4096 2>/dev/null
        openssl req -x509 -new -nodes \
            -key "$data_dir/staking/staker.key" \
            -sha256 -days 3650 \
            -out "$data_dir/staking/staker.crt" \
            -subj "/CN=node$node_num.$network_name.lux.network" 2>/dev/null
    fi

    # Bootstrap configuration for nodes 2-5
    local bootstrap_cmd=""
    if [ $node_num -gt 1 ]; then
        # Bootstrap from node 1
        bootstrap_cmd="--bootstrap-ips=127.0.0.1:$((base_port + 1)) --bootstrap-ids=NodeID-111111111111111111116DBWJs"
    fi

    # Launch the validator
    print_color "$BLUE" "  Starting $network_name validator $node_num (HTTP: $http_port, Staking: $staking_port)"

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
        $bootstrap_cmd \
        > "$data_dir/node.log" 2>&1 &

    local pid=$!
    echo $pid > "$data_dir/pid"

    # Small delay between node starts
    sleep 0.5
}

# Function to launch a complete network
launch_network() {
    local network_name=$1
    local network_id=$2
    local base_port=$3

    print_color "$GREEN" "\n🔷 Launching $network_name (Network ID: $network_id, Base Port: $base_port)"
    echo "-----------------------------------------------------"

    for i in $(seq 1 $VALIDATORS_PER_NETWORK); do
        launch_validator "$network_name" "$network_id" "$base_port" "$i"
    done

    print_color "$GREEN" "  ✅ $VALIDATORS_PER_NETWORK validators launched for $network_name"
}

# Function to stop all nodes
stop_all() {
    print_color "$YELLOW" "\n⚠️  Stopping all nodes..."
    pkill -f luxd || true
    sleep 2
    print_color "$GREEN" "✅ All nodes stopped"
}

# Function to check network health
check_health() {
    local port=$1
    local network=$2

    if curl -s "http://127.0.0.1:$port/ext/health" > /dev/null 2>&1; then
        local validators=$(curl -s -X POST -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","method":"platform.getCurrentValidators","params":{},"id":1}' \
            "http://127.0.0.1:$port/ext/P" 2>/dev/null | jq -r '.result.validators | length' 2>/dev/null || echo "0")
        print_color "$GREEN" "  $network: ✅ Healthy ($validators validators)"
    else
        print_color "$RED" "  $network: ❌ Not responding"
    fi
}

# Function to display status
show_status() {
    print_color "$BLUE" "\n📊 Network Status:"
    echo "=================="

    for network in "${!NETWORKS[@]}"; do
        IFS=':' read -r network_id base_port <<< "${NETWORKS[$network]}"
        check_health "$base_port" "$network"
    done
}

# Main execution
case "${1:-launch}" in
    launch)
        print_color "$YELLOW" "🚀 Launching ALL networks with $VALIDATORS_PER_NETWORK validators each"
        print_color "$YELLOW" "================================================\n"

        # Stop any existing nodes
        stop_all

        # Clean and create base directory
        rm -rf "$BASE_DATA_DIR"
        mkdir -p "$BASE_DATA_DIR"

        # Launch all networks
        for network in "${!NETWORKS[@]}"; do
            IFS=':' read -r network_id base_port <<< "${NETWORKS[$network]}"
            launch_network "$network" "$network_id" "$base_port"
        done

        print_color "$GREEN" "\n================================================"
        print_color "$GREEN" "✅ ALL NETWORKS LAUNCHED!"
        print_color "$GREEN" "================================================\n"

        # Wait for bootstrap
        print_color "$YELLOW" "⏳ Waiting for networks to bootstrap..."
        sleep 10

        # Show status
        show_status

        # Display summary
        print_color "$BLUE" "\n📈 Management Commands:"
        echo "----------------------"
        echo "  Status:  $0 status"
        echo "  Stop:    $0 stop"
        echo "  Logs:    tail -f $BASE_DATA_DIR/network-*/node*/node.log"
        echo ""
        print_color "$GREEN" "🎉 Total: $((VALIDATORS_PER_NETWORK * ${#NETWORKS[@]})) validators running in parallel!"
        ;;

    stop)
        stop_all
        ;;

    status)
        show_status
        ;;

    *)
        echo "Usage: $0 {launch|stop|status}"
        exit 1
        ;;
esac