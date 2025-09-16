#!/bin/bash

# This script fixes BLS SecretKey to Signer wrapping across test files correctly

echo "Fixing BLS SecretKey to Signer wrapping in test files (v2)..."

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
    
    # Remove any incorrect NewFromSecretKey calls
    sed -i 's/localsigner\.NewFromSecretKey/localsigner.FromBytes/g' "$file"
    
    # Fix warp.NewSigner calls with sk variables  
    sed -i 's/warp\.NewSigner(localsigner\.FromBytes(\(sk[0-9]*\)))/warp.NewSigner(signer)/g' "$file"
    sed -i 's/warp\.NewSigner(\(sk[0-9]*\))/warp.NewSigner(signer)/g' "$file"
    sed -i 's/warp\.NewSigner(sk)/warp.NewSigner(signer)/g' "$file"
    
    # Fix NewProofOfPossession calls
    sed -i 's/NewProofOfPossession(localsigner\.FromBytes(\(sk[0-9]*\)))/NewProofOfPossession(signer)/g' "$file"
    sed -i 's/NewProofOfPossession(\(sk[0-9]*\))/NewProofOfPossession(signer)/g' "$file"
    sed -i 's/NewProofOfPossession(sk)/NewProofOfPossession(signer)/g' "$file"
    
    # Fix NewIPSigner calls
    sed -i 's/NewIPSigner(\(.*\), \(.*\), localsigner\.FromBytes(bls))/NewIPSigner(\1, \2, signer)/g' "$file"
    sed -i 's/NewIPSigner(\(.*\), \(.*\), bls)/NewIPSigner(\1, \2, signer)/g' "$file"
    
    # Fix tt.ip.Sign calls
    sed -i 's/tt\.ip\.Sign(\(.*\), localsigner\.FromBytes(tt\.blsSigner))/tt.ip.Sign(\1, signer)/g' "$file"
    sed -i 's/tt\.ip\.Sign(\(.*\), tt\.blsSigner)/tt.ip.Sign(\1, signer)/g' "$file"
    
    echo "  Applied pattern replacements"
done

# Now add the proper signer creation code
echo "Adding proper signer creation code..."

# For aggregator_test.go - needs multiple signers
if [ -f "network/p2p/lp118/aggregator_test.go" ]; then
    echo "Fixing aggregator_test.go with multiple signers..."
    # This needs manual fixing since it has multiple signers
fi

# For handler_test.go
if [ -f "network/p2p/lp118/handler_test.go" ]; then
    echo "Fixing handler_test.go..."
    # This needs manual fixing
fi

echo "Done! Manual intervention needed for proper signer creation."
echo "Pattern to use: signer, _ := localsigner.FromBytes(bls.SecretKeyToBytes(sk))"