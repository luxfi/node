#!/bin/bash

# Fix all wallet examples to use the keychain adapter

cd /home/z/work/lux/node

# Find all Go files in wallet examples that use secp256k1fx.Keychain
examples=$(find wallet/subnet/primary/examples -name "*.go" -exec grep -l "secp256k1fx.NewKeychain" {} \;)

for file in $examples; do
    echo "Fixing $file"
    
    # Add keychain import if not present
    if ! grep -q '"github.com/luxfi/node/wallet/keychain"' "$file"; then
        sed -i '/"github.com\/luxfi\/node\/vms\/secp256k1fx"/a\\t"github.com/luxfi/node/wallet/keychain"' "$file"
    fi
    
    # Find the line with kc := secp256k1fx.NewKeychain and add adapter after it
    if grep -q "kc := secp256k1fx.NewKeychain" "$file"; then
        # Check if adapter already exists
        if ! grep -q "adapter := keychain.NewLedgerAdapter(kc)" "$file"; then
            sed -i '/kc := secp256k1fx.NewKeychain/a\\n\t// Create adapter for the keychain\n\tadapter := keychain.NewLedgerAdapter(kc)' "$file"
        fi
        
        # Replace LUXKeychain: kc with LUXKeychain: adapter
        sed -i 's/LUXKeychain: kc,/LUXKeychain: adapter,/g' "$file"
        
        # Replace EthKeychain: kc with EthKeychain: adapter
        sed -i 's/EthKeychain: kc,/EthKeychain: adapter,/g' "$file"
    fi
done

# Also fix the example_test.go file
testfile="wallet/subnet/primary/example_test.go"
if [ -f "$testfile" ]; then
    echo "Fixing $testfile"
    
    # Add keychain import if not present
    if ! grep -q '"github.com/luxfi/node/wallet/keychain"' "$testfile"; then
        sed -i '/"github.com\/luxfi\/node\/vms\/secp256k1fx"/a\\t"github.com/luxfi/node/wallet/keychain"' "$testfile"
    fi
    
    # Find the line with kc := secp256k1fx.NewKeychain and add adapter after it
    if grep -q "kc := secp256k1fx.NewKeychain" "$testfile"; then
        # Check if adapter already exists
        if ! grep -q "adapter := keychain.NewLedgerAdapter(kc)" "$testfile"; then
            sed -i '/kc := secp256k1fx.NewKeychain/a\\n\t// Create adapter for the keychain\n\tadapter := keychain.NewLedgerAdapter(kc)' "$testfile"
        fi
        
        # Replace LUXKeychain: kc with LUXKeychain: adapter
        sed -i 's/LUXKeychain: kc,/LUXKeychain: adapter,/g' "$testfile"
        
        # Replace EthKeychain: kc with EthKeychain: adapter
        sed -i 's/EthKeychain: kc,/EthKeychain: adapter,/g' "$testfile"
    fi
fi

echo "Done fixing wallet examples"