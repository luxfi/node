#!/bin/bash
# Replace package declarations in moved files
# Ensure we are fixing package names for moved files
# vm/manager was 'package manager', so it fits node/vms/manager
# vm/rpc was 'package rpc' (likely), needs to be 'package rpcchainvm'

if [ -d "vms/manager" ]; then
    sed -i '' 's/^package.*/package manager/' vms/manager/*.go
fi
if [ -d "vms/rpcchainvm" ]; then
    sed -i '' 's/^package.*/package rpcchainvm/' vms/rpcchainvm/*.go
fi

# Global replacements
DIRS=". ../precompile ../coreth ../evm ../rpc ../wallet ../staking"

for dir in $DIRS; do
    if [ -d "$dir" ]; then
        echo "Processing $dir..."
        find "$dir" -name "*.go" -type f -print0 | xargs -0 sed -i '' \
            -e 's|github.com/luxfi/consensus/runtime|github.com/luxfi/runtime|g' \
            -e 's|github.com/luxfi/consensus/validator|github.com/luxfi/validators|g' \
            -e 's|github.com/luxfi/consensus/version|github.com/luxfi/version|g' \
            -e 's|github.com/luxfi/vm/manager|github.com/luxfi/node/vms/manager|g' \
            -e 's|github.com/luxfi/vm/rpc|github.com/luxfi/node/vms/rpcchainvm|g' \
            -e 's|github.com/luxfi/vm/chains|github.com/luxfi/node/chains|g' \
            -e 's|version\.Current()|version.CurrentApp|g' \
            -e 's|consensusversion\.Current()|version.CurrentApp|g'
    fi
done
