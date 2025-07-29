#!/bin/bash

echo "Updating consensus/linear imports to consensus/chain..."

# Update import paths
find . -name "*.go" -type f -exec sed -i 's|"github.com/luxfi/node/consensus/linear"|"github.com/luxfi/node/consensus/chain"|g' {} +
find . -name "*.go" -type f -exec sed -i 's|"github.com/luxfi/node/consensus/engine/linear"|"github.com/luxfi/node/consensus/engine/chain"|g' {} +

# Update aliased imports
find . -name "*.go" -type f -exec sed -i 's|linear "github.com/luxfi/node/consensus/linear"|chain "github.com/luxfi/node/consensus/chain"|g' {} +

# Update references in comments and strings that might contain the old path
find . -name "*.go" -type f -exec sed -i 's|consensus/linear/|consensus/chain/|g' {} +
find . -name "*.go" -type f -exec sed -i 's|consensus/engine/linear/|consensus/engine/chain/|g' {} +

echo "Done updating imports!"