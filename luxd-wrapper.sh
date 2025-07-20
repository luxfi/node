#!/bin/bash
# Lux node wrapper with HD wallet support
# This allows running luxd with seed phrase and account index

set -e

# Default values
SEED=""
ACCOUNT_INDEX=0
LUXD_BINARY="${LUXD_BINARY:-$(dirname "$0")/build/luxd}"

# Parse wrapper-specific arguments
LUXD_ARGS=()
while [[ $# -gt 0 ]]; do
    case $1 in
        --seed)
            SEED="$2"
            shift 2
            ;;
        --account)
            ACCOUNT_INDEX="$2"
            shift 2
            ;;
        *)
            # Pass through to luxd
            LUXD_ARGS+=("$1")
            shift
            ;;
    esac
done

# If seed is provided, derive the node key
if [ -n "$SEED" ]; then
    # Create temporary Python script for key derivation
    DERIVE_SCRIPT=$(mktemp /tmp/derive_node_key_XXXXXX.py)
    cat > "$DERIVE_SCRIPT" << 'EOF'
#!/usr/bin/env python3
import sys
import os
import json
import hashlib
from eth_account import Account

# Enable HD wallet features
Account.enable_unaudited_hdwallet_features()

def derive_node_key(seed_phrase, account_index):
    """Derive node key from seed phrase and account index"""
    
    # Standard HD path for validators
    hd_path = f"m/44'/60'/0'/0/{account_index}"
    
    # Derive account
    account = Account.from_mnemonic(seed_phrase, account_path=hd_path)
    
    # Create a deterministic node ID from the account
    # This is a simplified version - in production you'd use proper key derivation
    node_key_seed = hashlib.sha256(f"{seed_phrase}{account_index}node".encode()).hexdigest()
    
    return {
        "account_index": account_index,
        "address": account.address,
        "private_key": account.key.hex(),
        "node_key_seed": node_key_seed
    }

if __name__ == "__main__":
    seed = os.environ.get('WALLET_SEED', '')
    if not seed:
        print("Error: WALLET_SEED environment variable not set", file=sys.stderr)
        sys.exit(1)
    
    account_idx = int(os.environ.get('WALLET_ACCOUNT', '0'))
    
    try:
        result = derive_node_key(seed, account_idx)
        print(json.dumps(result))
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)
EOF

    # Export for Python script
    export WALLET_SEED="$SEED"
    export WALLET_ACCOUNT="$ACCOUNT_INDEX"
    
    # Derive keys
    NODE_INFO=$(python3 "$DERIVE_SCRIPT" 2>/dev/null)
    rm -f "$DERIVE_SCRIPT"
    
    if [ -z "$NODE_INFO" ]; then
        echo "Error: Failed to derive node keys"
        exit 1
    fi
    
    # Extract values
    NODE_ADDRESS=$(echo "$NODE_INFO" | jq -r '.address')
    
    # Set up staking directory if needed
    DATA_DIR=""
    for arg in "${LUXD_ARGS[@]}"; do
        if [[ "$arg" == --data-dir=* ]]; then
            DATA_DIR="${arg#--data-dir=}"
            break
        fi
    done
    
    if [ -n "$DATA_DIR" ] && [ ! -f "$DATA_DIR/staking/staker.key" ]; then
        echo "Setting up staking keys for account $ACCOUNT_INDEX ($NODE_ADDRESS)..."
        mkdir -p "$DATA_DIR/staking"
        
        # Generate deterministic staking keys based on account
        cd "$DATA_DIR/staking"
        
        # Use a deterministic approach for the staking key
        # In production, this should use proper key derivation
        echo "$NODE_INFO" | jq -r '.node_key_seed' | openssl dgst -sha256 -binary > seed.bin
        openssl genrsa -rand seed.bin -out staker.key 4096 2>/dev/null
        openssl req -new -x509 -key staker.key -out staker.crt -days 365 \
            -subj "/C=US/ST=NY/O=Lux/CN=validator-$ACCOUNT_INDEX" 2>/dev/null
        rm -f seed.bin
        
        # Save node info
        echo "$NODE_INFO" | jq 'del(.private_key, .node_key_seed)' > "$DATA_DIR/node-info.json"
    fi
    
    # Clear sensitive data
    unset WALLET_SEED
    unset NODE_INFO
fi

# Run luxd with the original arguments
exec "$LUXD_BINARY" "${LUXD_ARGS[@]}"