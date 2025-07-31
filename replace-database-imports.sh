#!/bin/bash

# Replace internal database imports with external luxfi/database package

echo "Replacing internal database imports with external luxfi/database..."

# Replace imports
find . -name "*.go" -type f -exec sed -i 's|"github.com/luxfi/node/database"|"github.com/luxfi/database"|g' {} \;
find . -name "*.go" -type f -exec sed -i 's|"github.com/luxfi/node/database/|"github.com/luxfi/database/|g' {} \;

# Also need to update the RPC database references
find . -name "*.go" -type f -exec sed -i 's|github.com/luxfi/node/database/rpcdb|github.com/luxfi/database/rpcdb|g' {} \;

echo "Done replacing database imports"