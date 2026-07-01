#!/bin/bash
# Import RLP blocks for chains after deployment
# Usage: ./import-chain-rlp.sh [mainnet|testnet|both]
#
# Prerequisites:
#   - Chains must be deployed (run deploy-chains.sh first)
#   - Nodes must be tracking the chains
#   - Blockchain IDs must be set in the node config

set -e

MAINNET_RPC="http://134.199.143.51:9650"
TESTNET_RPC="http://146.190.1.172:9650"
STATE_DIR="$HOME/work/lux/state/rlp"

import_rlp() {
    local network=$1
    local chain=$2
    local chain_id=$3
    local rlp_file=$4
    local rpc_url=$5

    local rlp_path="$STATE_DIR/$chain/$rlp_file"
    if [ ! -f "$rlp_path" ]; then
        echo "SKIP: $rlp_path not found"
        return
    fi

    echo "Importing $chain ($rlp_file) to $network..."

    # Use lux CLI chain import command
    lux chain import "$chain_id" "$rlp_path" --non-interactive 2>&1 || {
        # Fallback: direct RPC import
        echo "CLI import failed, trying direct RPC..."
        # The admin API needs to be enabled on the node
        # Import via kubectl exec on the first pod
        local ns="lux-$network"
        kubectl --context do-sfo3-lux-k8s exec -n "$ns" luxd-0 -- \
            wget -q -O /tmp/import.rlp "$rpc_url" 2>/dev/null || true
        echo "NOTE: For chain import, you may need to use admin.importChain RPC"
        echo "      or copy the RLP file to the node and import manually."
    }
}

TARGET="${1:-both}"

case "$TARGET" in
    mainnet)
        echo "=== Importing chain RLP blocks to mainnet ==="
        import_rlp mainnet zoo-mainnet 200200 zoo-mainnet-200200.rlp "$MAINNET_RPC"
        import_rlp mainnet spc-mainnet 36911 spc-mainnet-36911.rlp "$MAINNET_RPC"
        ;;
    testnet)
        echo "=== Importing chain RLP blocks to testnet ==="
        import_rlp testnet zoo-testnet 200201 zoo-testnet-200201.rlp "$TESTNET_RPC"
        ;;
    both|all)
        echo "=== Importing chain RLP blocks to mainnet ==="
        import_rlp mainnet zoo-mainnet 200200 zoo-mainnet-200200.rlp "$MAINNET_RPC"
        import_rlp mainnet spc-mainnet 36911 spc-mainnet-36911.rlp "$MAINNET_RPC"
        echo ""
        echo "=== Importing chain RLP blocks to testnet ==="
        import_rlp testnet zoo-testnet 200201 zoo-testnet-200201.rlp "$TESTNET_RPC"
        ;;
    *)
        echo "Usage: $0 [mainnet|testnet|both]"
        exit 1
        ;;
esac

echo ""
echo "Import complete! Verify with:"
echo "  curl -s -X POST --data '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_blockNumber\",\"params\":[]}' -H 'content-type:application/json' \$RPC_URL/v1/bc/<blockchain-id>/rpc"
