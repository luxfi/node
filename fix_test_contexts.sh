#!/bin/bash

# Fix all test files that use constants.PlatformChainID
for file in vms/platformvm/txs/*_test.go; do
    if grep -q "constants.PlatformChainID" "$file"; then
        echo "Fixing $file"
        
        # Add testChainID generation before context setup
        sed -i '/ctx := context.Background()/i\	testChainID := ids.GenerateTestID() // Use a test chain ID instead of empty' "$file"
        
        # Replace constants.PlatformChainID with testChainID
        sed -i 's/ChainID:[ ]*constants\.PlatformChainID/ChainID:    testChainID/g' "$file"
        
        # For cases where testChainID might already exist, avoid duplicate
        sed -i '/testChainID := ids.GenerateTestID().*\/\/ Use a test chain ID/N;s/\n\ttestChainID := ids.GenerateTestID().*\/\/ Use a test chain ID//' "$file"
    fi
done

echo "Fixed all test files"