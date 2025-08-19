#!/bin/bash

cd /home/z/work/lux/node

# Fix examples with empty keychains
files=(
    "wallet/subnet/primary/examples/register-l1-validator/main.go"
    "wallet/subnet/primary/examples/increase-l1-validator-balance/main.go"
    "wallet/subnet/primary/examples/convert-subnet-to-l1/main.go"
    "wallet/subnet/primary/examples/set-l1-validator-weight/main.go"
)

for file in "${files[@]}"; do
    echo "Fixing $file - empty keychain"
    # Replace secp256k1fx.NewKeychain() with keychain.NewLedgerAdapter(secp256k1fx.NewKeychain())
    sed -i 's/EthKeychain:[ ]*secp256k1fx\.NewKeychain()/EthKeychain: keychain.NewLedgerAdapter(secp256k1fx.NewKeychain())/g' "$file"
done

# Fix examples using kc directly instead of adapter
files=(
    "wallet/subnet/primary/examples/create-chain/main.go"
    "wallet/subnet/primary/examples/add-permissioned-subnet-validator/main.go"
)

for file in "${files[@]}"; do
    echo "Fixing $file - using kc instead of adapter"
    # Replace LUXKeychain: kc with LUXKeychain: adapter
    sed -i 's/LUXKeychain:[ ]*kc,/LUXKeychain: adapter,/g' "$file"
    # Replace EthKeychain: kc with EthKeychain: adapter
    sed -i 's/EthKeychain:[ ]*kc,/EthKeychain: adapter,/g' "$file"
done

# Fix sign examples
sign_files=(
    "wallet/subnet/primary/examples/sign-l1-validator-removal-genesis/main.go"
    "wallet/subnet/primary/examples/sign-l1-validator-removal-registration/main.go"
)

for file in "${sign_files[@]}"; do
    echo "Fixing $file - sign examples"
    if grep -q "EthKeychain:[ ]*secp256k1fx\.NewKeychain()" "$file"; then
        sed -i 's/EthKeychain:[ ]*secp256k1fx\.NewKeychain()/EthKeychain: keychain.NewLedgerAdapter(secp256k1fx.NewKeychain())/g' "$file"
    fi
done

echo "Done fixing remaining examples"