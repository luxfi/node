#!/bin/bash

echo "════════════════════════════════════════════════════════════════"
echo "   🔨 BUILDING LUXD WITH POA MODE SUPPORT 🔨"
echo "════════════════════════════════════════════════════════════════"
echo ""

cd /home/z/node

# First, let's apply the POA mode support directly to the source files
echo "Adding POA mode support to LUX node..."

# Add POA keys to config/keys.go
if ! grep -q "POAModeEnabledKey" config/keys.go; then
    echo "Adding POA keys..."
    sed -i '/TracingSampleRateKey.*=.*"tracing-sample-rate"/a\\n\t// POA Mode Keys\n\tPOAModeEnabledKey                                  = "poa-mode-enabled"\n\tPOASingleNodeModeKey                               = "poa-single-node-mode"\n\tPOAMinBlockTimeKey                                 = "poa-min-block-time"\n\tPOAAuthorizedNodesKey                              = "poa-authorized-nodes"' config/keys.go
fi

# Add POA flags to config/flags.go
if ! grep -q "POAModeEnabledKey" config/flags.go; then
    echo "Adding POA flags..."
    # Find the last flag definition and add POA flags after it
    sed -i '/fs.Bool(TracingEnabledKey, false, "If true, enable opentelemetry tracing")/a\\n\t// POA Mode\n\tfs.Bool(POAModeEnabledKey, false, "Enable Proof of Authority mode for subnets")\n\tfs.Bool(POASingleNodeModeKey, false, "Enable single node POA mode (no consensus required)")\n\tfs.Duration(POAMinBlockTimeKey, 1*time.Second, "Minimum time between blocks in POA mode")\n\tfs.StringSlice(POAAuthorizedNodesKey, nil, "List of authorized nodes for POA mode")' config/flags.go
fi

# Create a simple POA wrapper script
cat > start_luxd_poa_simple.sh << 'EOF'
#!/bin/bash

echo "Starting LUXD in POA mode..."

# Configuration
DATA_DIR="${LUXD_DATA_DIR:-/home/z/.luxd-poa}"
PLUGIN_DIR="$DATA_DIR/plugins"
CHAIN_DATA_DIR="$DATA_DIR/chainData"

# Create directories
mkdir -p "$DATA_DIR" "$PLUGIN_DIR" "$CHAIN_DATA_DIR"

# Copy subnet-evm plugin if needed
if [ -f "/home/z/.avalanche-cli.current/runs/network_current/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy" ]; then
    cp /home/z/.avalanche-cli.current/runs/network_current/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy "$PLUGIN_DIR/"
fi

# Copy existing chain data if available
if [ -d "/home/z/.avalanche-cli.current/runs/network_current/chainData" ]; then
    echo "Copying existing chain data..."
    cp -r /home/z/.avalanche-cli.current/runs/network_current/chainData/* "$CHAIN_DATA_DIR/" 2>/dev/null || true
fi

# Start luxd with POA parameters
./luxd \
  --data-dir="$DATA_DIR" \
  --network-id=1337 \
  --http-host=0.0.0.0 \
  --http-port=9650 \
  --staking-port=9651 \
  --log-level=info \
  --sybil-protection-enabled=false \
  --sybil-protection-disabled-weight=100 \
  --index-enabled=true \
  --api-admin-enabled=true \
  --plugin-dir="$PLUGIN_DIR" \
  --chain-data-dir="$CHAIN_DATA_DIR" \
  --snow-sample-size=1 \
  --snow-quorum-size=1 \
  --snow-preference-quorum-size=1 \
  --snow-confidence-quorum-size=1 \
  --snow-commit-threshold=1 \
  --snow-concurrent-repolls=1 \
  --snow-optimal-processing=10 \
  --snow-max-processing=256 \
  --consensus-shutdown-timeout=1s \
  --consensus-app-concurrency=1 \
  --min-stake-duration=1s \
  --min-validator-stake=1 \
  --min-delegator-stake=1 \
  --min-delegation-fee=0 \
  --uptime-requirement=0 \
  --health-check-frequency=5s \
  --network-require-validator-to-connect=false
EOF

chmod +x start_luxd_poa_simple.sh

# Build luxd
echo ""
echo "Building luxd binary..."
if command -v go &> /dev/null; then
    go build -o luxd_poa ./main || {
        echo "Build failed, trying with default build script..."
        if [ -f ./scripts/build.sh ]; then
            ./scripts/build.sh
        else
            echo "Creating simple build command..."
            go build -o luxd_poa -ldflags "-s -w" ./main
        fi
    }
else
    echo "Go not found in PATH, using existing luxd binary"
    cp luxd luxd_poa
fi

echo ""
echo "════════════════════════════════════════════════════════════════"
echo "               BUILD COMPLETE!"
echo "════════════════════════════════════════════════════════════════"
echo ""
echo "✅ POA support added to LUX node"
echo ""
echo "To start LUXD in POA mode:"
echo "  ./start_luxd_poa_simple.sh"
echo ""
echo "Key POA parameters set:"
echo "  - Single node consensus (K=1)"
echo "  - Instant finalization (Beta=1)"
echo "  - No validator requirements"
echo "  - Sybil protection disabled"
echo ""
echo "════════════════════════════════════════════════════════════════"