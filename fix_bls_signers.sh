#!/bin/bash

# This script fixes BLS SecretKey to Signer wrapping across test files

echo "Fixing BLS SecretKey to Signer wrapping in test files..."

# Files that need fixing based on the errors
files=(
    "network/p2p/lp118/aggregator_test.go"
    "network/p2p/lp118/handler_test.go"
    "network/peer/ip_test.go"
    "network/peer/peer_test.go"
    "vms/platformvm/signer/bls_k1_test.go"
    "vms/platformvm/warp/signer_test.go"
    "vms/platformvm/txs/add_permissionless_validator_tx_test.go"
)

for file in "${files[@]}"; do
    echo "Processing $file..."
    
    # Check if file exists
    if [ ! -f "$file" ]; then
        echo "  File not found, skipping..."
        continue
    fi
    
    # Add localsigner import if not already present
    if ! grep -q "github.com/luxfi/node/utils/crypto/bls/signer/localsigner" "$file"; then
        # Find the import block and add the localsigner import
        sed -i '/^import (/a\\t"github.com/luxfi/node/utils/crypto/bls/signer/localsigner"' "$file"
        echo "  Added localsigner import"
    fi
    
    # Pattern replacements for common cases
    # These are generic patterns that should work for most cases
    
    # For warp.NewSigner calls
    sed -i 's/warp\.NewSigner(\(sk[0-9]*\))/warp.NewSigner(localsigner.NewFromSecretKey(\1))/g' "$file"
    
    # For NewProofOfPossession calls  
    sed -i 's/NewProofOfPossession(\(sk[0-9]*\))/NewProofOfPossession(localsigner.NewFromSecretKey(\1))/g' "$file"
    sed -i 's/NewProofOfPossession(sk)/NewProofOfPossession(localsigner.NewFromSecretKey(sk))/g' "$file"
    
    # For NewIPSigner calls with bls variable
    sed -i 's/NewIPSigner(\(.*\), \(.*\), bls)/NewIPSigner(\1, \2, localsigner.NewFromSecretKey(bls))/g' "$file"
    
    # For tt.ip.Sign calls
    sed -i 's/tt\.ip\.Sign(\(.*\), tt\.blsSigner)/tt.ip.Sign(\1, localsigner.NewFromSecretKey(tt.blsSigner))/g' "$file"
    
    echo "  Applied pattern replacements"
done

echo "Done! Please run 'go test ./...' to verify the fixes."