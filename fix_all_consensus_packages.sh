#!/bin/bash

echo "Fixing all consensus package declarations and imports..."

# Fix package declarations in moved files
find consensus/sampling -name "*slush*.go" -o -name "consensus*.go" -o -name "factory.go" -o -name "flat*.go" -o -name "network*.go" -o -name "parameters*.go" -o -name "tree*.go" | xargs sed -i 's/^package binaryvote$/package sampling/g'
find consensus/threshold -name "*snowflake*.go" | xargs sed -i 's/^package binaryvote$/package threshold/g'
find consensus/confidence -name "*snowball*.go" | xargs sed -i 's/^package binaryvote$/package confidence/g'

# Update imports throughout the codebase
find . -name "*.go" -type f | while read -r file; do
    # Skip vendor directories
    if [[ "$file" == *"/vendor/"* ]]; then
        continue
    fi
    
    # Update imports
    sed -i 's|"github.com/luxfi/node/consensus/binaryvote|"github.com/luxfi/node/consensus/sampling|g' "$file"
done

# Fix references to functions that moved between packages
find consensus/threshold -name "*.go" -type f -exec sed -i 's/newBinarySlush/sampling.NewBinarySampler/g' {} \;
find consensus/threshold -name "*.go" -type f -exec sed -i 's/newNnarySlush/sampling.NewMultiSampler/g' {} \;

# Add sampling import to threshold files that need it
for file in consensus/threshold/{binary,nnary}.go; do
    if ! grep -q '"github.com/luxfi/node/consensus/sampling"' "$file"; then
        # Add import after package declaration
        sed -i '/^package threshold$/a\\nimport (\n\t"github.com/luxfi/node/consensus/sampling"\n)' "$file"
    fi
done

echo "Package fixes complete."