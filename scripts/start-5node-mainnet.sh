#!/bin/bash
set -e

LUXD="/Users/z/work/lux/node/build/luxd"
BASE_DIR="/Users/z/.lux"
NETWORK_ID=96369

# Node port configurations (HTTP:STAKING)
NODE1_HTTP=9630; NODE1_STAKING=9631
NODE2_HTTP=9640; NODE2_STAKING=9641
NODE3_HTTP=9650; NODE3_STAKING=9651
NODE4_HTTP=9660; NODE4_STAKING=9661
NODE5_HTTP=9670; NODE5_STAKING=9671

# NodeIDs matching generated certificates
NODE1_ID="NodeID-3BYqaUaDKyv1X8ViLSjasmmQ2fhh35DHL"
NODE2_ID="NodeID-7fiqgBJiEKWuutSKcp2H8o242cbnR4WL"
NODE3_ID="NodeID-9AGiCTN2xERjjgkHvQ2RyzvXFFTo8gTcs"
NODE4_ID="NodeID-CTVbU8hx6Mbeuiaukppch7sXPQnJawL4t"
NODE5_ID="NodeID-5GU9HEo5WMhArUqcasSjFCEyj75tbLib1"

echo "Starting 5-node Lux Mainnet validator network (Network ID: $NETWORK_ID)"
echo "NO POA mode - Real validator consensus"

# Kill any existing luxd processes
pkill -f luxd 2>/dev/null || true
sleep 2

# Function to get HTTP port for node
get_http_port() {
    case $1 in
        1) echo $NODE1_HTTP ;;
        2) echo $NODE2_HTTP ;;
        3) echo $NODE3_HTTP ;;
        4) echo $NODE4_HTTP ;;
        5) echo $NODE5_HTTP ;;
    esac
}

# Function to get staking port for node
get_staking_port() {
    case $1 in
        1) echo $NODE1_STAKING ;;
        2) echo $NODE2_STAKING ;;
        3) echo $NODE3_STAKING ;;
        4) echo $NODE4_STAKING ;;
        5) echo $NODE5_STAKING ;;
    esac
}

# Function to get NodeID for node
get_node_id() {
    case $1 in
        1) echo $NODE1_ID ;;
        2) echo $NODE2_ID ;;
        3) echo $NODE3_ID ;;
        4) echo $NODE4_ID ;;
        5) echo $NODE5_ID ;;
    esac
}

# Start each node
for i in 1 2 3 4 5; do
    HTTP_PORT=$(get_http_port $i)
    STAKING_PORT=$(get_staking_port $i)

    DATA_DIR="$BASE_DIR/node$i"
    STAKING_DIR="$DATA_DIR/staking"
    LOG_FILE="$DATA_DIR/luxd.log"

    # Build bootstrap IPs and IDs (all other nodes)
    BOOTSTRAP_IPS=""
    BOOTSTRAP_IDS=""
    for j in 1 2 3 4 5; do
        if [ $j -ne $i ]; then
            OTHER_STAKING_PORT=$(get_staking_port $j)
            OTHER_NODE_ID=$(get_node_id $j)
            if [ -n "$BOOTSTRAP_IPS" ]; then
                BOOTSTRAP_IPS="$BOOTSTRAP_IPS,"
                BOOTSTRAP_IDS="$BOOTSTRAP_IDS,"
            fi
            BOOTSTRAP_IPS="${BOOTSTRAP_IPS}127.0.0.1:${OTHER_STAKING_PORT}"
            BOOTSTRAP_IDS="${BOOTSTRAP_IDS}${OTHER_NODE_ID}"
        fi
    done

    # Create log directory if needed
    mkdir -p "$DATA_DIR/logs"

    echo "Starting Node $i on ports HTTP=$HTTP_PORT, Staking=$STAKING_PORT"
    echo "  NodeID: $(get_node_id $i)"
    echo "  Bootstrap IPs: $BOOTSTRAP_IPS"

    $LUXD \
        --network-id=$NETWORK_ID \
        --http-port=$HTTP_PORT \
        --staking-port=$STAKING_PORT \
        --http-host=127.0.0.1 \
        --staking-tls-cert-file=$STAKING_DIR/staker.crt \
        --staking-tls-key-file=$STAKING_DIR/staker.key \
        --data-dir=$DATA_DIR \
        --log-dir=$DATA_DIR/logs \
        --log-level=info \
        --bootstrap-ips=$BOOTSTRAP_IPS \
        --bootstrap-ids=$BOOTSTRAP_IDS \
        --staking-enabled=true \
        --sybil-protection-enabled=true \
        > $LOG_FILE 2>&1 &

    echo "  PID: $!"
    sleep 1
done

echo ""
echo "All 5 nodes started!"
echo "Waiting 15 seconds for nodes to initialize..."
sleep 15

# Check node health
echo ""
echo "Checking node health..."
for i in 1 2 3 4 5; do
    HTTP_PORT=$(get_http_port $i)
    echo -n "Node $i (port $HTTP_PORT): "
    curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"health.health"}' \
        -H 'content-type:application/json;' \
        http://127.0.0.1:$HTTP_PORT/ext/health 2>/dev/null | head -c 200 || echo "Not responding yet"
    echo ""
done

echo ""
echo "Check logs at: $BASE_DIR/node*/luxd.log"
