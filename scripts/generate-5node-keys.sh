#!/bin/bash
# Generate persistent staking keys for 5-node network
# These keys should be stored and reused for consistent node IDs

set -e

LUXD_BIN="${LUXD_BIN:-/Users/z/work/lux/node/build/luxd}"
KEYS_DIR="${KEYS_DIR:-/Users/z/work/lux/keys}"

echo "=============================================="
echo "  Generating 5-Node Staking Keys"
echo "=============================================="

# Create keys directory
mkdir -p "${KEYS_DIR}"

# Function to generate staking key and extract node ID
generate_node_key() {
    local node_num=$1
    local node_dir="${KEYS_DIR}/node${node_num}"

    mkdir -p "${node_dir}"

    # Generate TLS key pair
    echo "Generating keys for node${node_num}..."
    openssl req -x509 -newkey rsa:4096 -sha256 -days 3650 \
        -keyout "${node_dir}/staker.key" \
        -out "${node_dir}/staker.crt" \
        -subj "/CN=luxd-node${node_num}" \
        -nodes 2>/dev/null

    echo "  Keys saved to: ${node_dir}/"
}

# Generate keys for all 5 nodes
for i in {1..5}; do
    generate_node_key "$i"
done

echo ""
echo "=============================================="
echo "  Keys Generated Successfully!"
echo "=============================================="
echo ""
echo "Keys location: ${KEYS_DIR}"
echo ""
echo "Next steps:"
echo "1. Start temporary node with each key to get Node ID"
echo "2. Update genesis with all 5 Node IDs as validators"
echo "3. Start 5-node network with persistent keys"
echo ""
echo "Run: ./scripts/get-node-ids.sh to extract Node IDs"
