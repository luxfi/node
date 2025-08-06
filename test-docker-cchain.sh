#!/bin/bash
set -e

echo "🧪 Testing luxd Docker container with C-chain historic data"
echo "=========================================================="

# Stop any existing test container
docker stop luxd-cchain-test 2>/dev/null || true
docker rm luxd-cchain-test 2>/dev/null || true

# Kill any existing luxd process on port 9630
echo "📋 Checking for existing processes..."
if lsof -i :9630 >/dev/null 2>&1; then
    echo "⚠️  Port 9630 is in use. Please stop the existing process first."
    echo "   Run: pkill -f luxd"
    exit 1
fi

# Create test directories
TEST_DIR="./test-luxd-docker"
rm -rf $TEST_DIR
mkdir -p $TEST_DIR/{staking,logs,data}

# Copy test staking keys if they exist
if [ -d "/home/z/work/lux/node/staking/local" ]; then
    cp /home/z/work/lux/node/staking/local/staker1.* $TEST_DIR/staking/
    echo "✅ Copied staking keys"
else
    echo "⚠️  No staking keys found, generating temporary ones..."
    # Generate temporary self-signed certificate for testing
    openssl req -x509 -newkey rsa:4096 -keyout $TEST_DIR/staking/staker.key \
        -out $TEST_DIR/staking/staker.crt -days 365 -nodes \
        -subj "/C=US/ST=State/L=City/O=Test/CN=test.local" 2>/dev/null
fi

echo ""
echo "🚀 Starting luxd container with C-chain data..."
echo "   - C-chain data: /home/z/work/lux/state/cchain (1,082,781 blocks)"
echo "   - Network ID: 96369"
echo "   - RPC Port: 9630"
echo "   - Staking Port: 9631"
echo ""

# Run the container
docker run -d \
    --name luxd-cchain-test \
    -p 9630:9630 \
    -p 9631:9631 \
    -v /home/z/work/lux/state/cchain:/blockchain-data:ro \
    -v $PWD/$TEST_DIR/staking:/keys/staking:ro \
    -v $PWD/$TEST_DIR/logs:/logs \
    -v $PWD/$TEST_DIR/data:/data \
    -e NETWORK_ID=96369 \
    -e LOG_LEVEL=debug \
    -e STAKING_ENABLED=false \
    -e SYBIL_PROTECTION_ENABLED=false \
    ghcr.io/luxfi/node:local

echo "⏳ Waiting for container to start..."
sleep 5

# Check container status
if ! docker ps | grep -q luxd-cchain-test; then
    echo "❌ Container failed to start!"
    echo "📋 Container logs:"
    docker logs luxd-cchain-test
    exit 1
fi

echo "✅ Container is running"
echo ""
echo "📊 Container logs (last 20 lines):"
docker logs --tail 20 luxd-cchain-test

echo ""
echo "🔍 Testing API endpoints..."
echo ""

# Function to test endpoint
test_endpoint() {
    local name=$1
    local url=$2
    local data=$3
    
    echo -n "   Testing $name... "
    response=$(curl -s -X POST -H "Content-Type: application/json" -d "$data" "$url" 2>/dev/null || echo "FAILED")
    
    if [[ "$response" == "FAILED" ]] || [[ "$response" == *"error"* ]] || [[ "$response" == *"rejected"* ]]; then
        echo "⚠️  Not ready: $response"
    else
        echo "✅ OK: $response"
    fi
}

# Wait for bootstrap
echo "⏳ Waiting for node to bootstrap (this may take a minute)..."
for i in {1..30}; do
    response=$(curl -s -X POST -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","id":1,"method":"info.isBootstrapped","params":{"chain":"C"}}' \
        http://localhost:9630/ext/info 2>/dev/null || echo "{}")
    
    if [[ "$response" == *'"isBootstrapped":true'* ]]; then
        echo "✅ Node is bootstrapped!"
        break
    fi
    
    if [ $i -eq 30 ]; then
        echo "⚠️  Node is still bootstrapping. This is normal for first run."
        echo "   You can check status with: curl http://localhost:9630/ext/info"
    else
        echo -n "."
        sleep 2
    fi
done

echo ""
echo "📡 Testing RPC endpoints:"
test_endpoint "Health" "http://localhost:9630/ext/health" ""
test_endpoint "Node Info" "http://localhost:9630/ext/info" '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID","params":{}}'
test_endpoint "Block Number" "http://localhost:9630/ext/bc/C/rpc" '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}'
test_endpoint "Chain ID" "http://localhost:9630/ext/bc/C/rpc" '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}'
test_endpoint "Latest Block" "http://localhost:9630/ext/bc/C/rpc" '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["latest",false]}'

echo ""
echo "📈 Checking loaded blockchain data:"
# Try to get a specific historical block
test_endpoint "Block 1000" "http://localhost:9630/ext/bc/C/rpc" '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0x3e8",false]}'
test_endpoint "Block 100000" "http://localhost:9630/ext/bc/C/rpc" '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0x186a0",false]}'

echo ""
echo "🎉 Test complete!"
echo ""
echo "📝 Commands to interact with the container:"
echo "   - View logs:        docker logs -f luxd-cchain-test"
echo "   - Stop container:   docker stop luxd-cchain-test"
echo "   - Remove container: docker rm luxd-cchain-test"
echo "   - Shell access:     docker exec -it luxd-cchain-test bash"
echo ""
echo "🌐 API Endpoints:"
echo "   - RPC:     http://localhost:9630/ext/bc/C/rpc"
echo "   - WS:      ws://localhost:9630/ext/bc/C/ws"
echo "   - Health:  http://localhost:9630/ext/health"
echo "   - Info:    http://localhost:9630/ext/info"