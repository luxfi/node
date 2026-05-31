#!/bin/bash
# Deploy all 4 app chains (Zoo, Hanzo, SPC, Pars) to mainnet + testnet
# Usage: ./deploy-chains.sh [mainnet|testnet|devnet|both]
#
# Prerequisites:
#   - macOS keychain must have mainnet-key-02 key (will prompt for approval)
#   - Nodes must be running on the target network
#   - Genesis files must exist in ~/work/lux/state/chains/

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_BIN="$SCRIPT_DIR/deploy-chains"
STATE_DIR="$HOME/work/lux/state/chains"

# Build if needed
if [ ! -f "$DEPLOY_BIN" ]; then
    echo "Building deploy-chains..."
    cd "$SCRIPT_DIR"
    go build -o "$DEPLOY_BIN" ./wallet/network/primary/examples/deploy-chains/
fi

# Use mainnet-key-02 (NOT 01!) because 01's funds are all staked by initialStakers
# mainnet-key-02 has 70M LUX unlocked on P-chain, sufficient for chain creation
KEY_NAME="mainnet-key-02"
echo "Extracting $KEY_NAME from keychain..."
echo "(Approve the macOS keychain dialog that appears)"
KEY=$(security find-generic-password -s "io.lux.cli" -a "lux-key-$KEY_NAME" -w 2>/dev/null)
if [ -z "$KEY" ]; then
    echo "ERROR: Could not extract key from keychain."
    echo "Make sure $KEY_NAME exists: security find-generic-password -s io.lux.cli -a lux-key-$KEY_NAME"
    exit 1
fi
echo "Key loaded successfully ($KEY_NAME)."

TARGET="${1:-both}"

deploy_network() {
    local network=$1
    echo ""
    echo "=============================="
    echo "Deploying chains to $network"
    echo "=============================="

    PRIVATE_KEY="$KEY" "$DEPLOY_BIN" \
        --network="$network" \
        --state-dir="$STATE_DIR" 2>&1

    echo ""
    echo "$network deployment complete!"
}

case "$TARGET" in
    mainnet)
        deploy_network mainnet
        ;;
    testnet)
        deploy_network testnet
        ;;
    devnet)
        deploy_network devnet
        ;;
    both|all)
        deploy_network mainnet
        deploy_network testnet
        ;;
    *)
        echo "Usage: $0 [mainnet|testnet|devnet|both]"
        exit 1
        ;;
esac

# Clear the key from memory
unset KEY

echo ""
echo "All deployments complete!"
echo ""
echo "Next steps:"
echo "  1. Copy the Chain IDs and Blockchain IDs from output above"
echo "  2. Update Helm values files:"
echo "     ~/work/lux/devops/charts/lux/values-mainnet.yaml"
echo "     ~/work/lux/devops/charts/lux/values-testnet.yaml"
echo "  3. Helm upgrade to apply new chain tracking config"
echo "  4. Import RLP blocks for Zoo/SPC if available"
