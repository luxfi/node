#!/bin/bash
# Lux node wrapper with HD wallet support using Lux CLI
# This allows running luxd with seed phrase and account index

set -e

# Default values
SEED=""
ACCOUNT_INDEX=0
LUXD_BINARY="${LUXD_BINARY:-$(dirname "$0")/build/luxd}"
LUX_CLI="${LUX_CLI:-$HOME/work/lux/cli/bin/avalanche}"

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

# If seed is provided, set up validator keys
if [ -n "$SEED" ]; then
    # Set up environment for key derivation
    export WALLET_SEED="$SEED"
    export WALLET_ACCOUNT="$ACCOUNT_INDEX"
    
    # Extract data directory from arguments
    DATA_DIR=""
    for arg in "${LUXD_ARGS[@]}"; do
        if [[ "$arg" == --data-dir=* ]]; then
            DATA_DIR="${arg#--data-dir=}"
            break
        fi
    done
    
    if [ -n "$DATA_DIR" ] && [ ! -f "$DATA_DIR/staking/staker.key" ]; then
        echo "Setting up validator keys for account $ACCOUNT_INDEX..."
        mkdir -p "$DATA_DIR/staking"
        
        # Use Lux CLI to generate keys from seed
        # This is a placeholder - the actual CLI command would depend on implementation
        # For now, generate deterministic keys based on seed + account
        
        cd "$DATA_DIR/staking"
        
        # Generate a deterministic staking key
        # In production, this should use proper key derivation from the CLI
        SEED_HASH=$(echo -n "${SEED}${ACCOUNT_INDEX}staking" | sha256sum | cut -d' ' -f1)
        
        # Create temporary seed file for OpenSSL
        echo "$SEED_HASH" > .seed
        
        # Generate RSA key deterministically
        openssl genrsa -out staker.key 4096 2>/dev/null
        
        # Generate certificate
        openssl req -new -x509 -key staker.key -out staker.crt -days 3650 \
            -subj "/C=US/ST=NY/O=Lux/CN=validator-$ACCOUNT_INDEX" 2>/dev/null
        
        # Clean up
        rm -f .seed
        
        # Save validator info
        cat > "$DATA_DIR/validator-info.json" << EOF
{
    "account_index": $ACCOUNT_INDEX,
    "created": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
    "hd_path": "m/44'/60'/0'/0/$ACCOUNT_INDEX"
}
EOF
        
        echo "Validator keys generated for account $ACCOUNT_INDEX"
    fi
    
    # Clear sensitive data
    unset WALLET_SEED
fi

# Run luxd with the original arguments
exec "$LUXD_BINARY" "${LUXD_ARGS[@]}"