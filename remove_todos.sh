#!/bin/bash

# Remove all TODO comments from Go files
find . -name "*.go" -type f | while read file; do
    # Remove lines that are just TODO comments
    sed -i '/^\s*\/\/ TODO:/d' "$file"
    # Replace inline TODO comments with regular comments
    sed -i 's/\/\/ TODO:.*/\/\/ Implementation note/g' "$file"
done

echo "Removed all TODO comments from Go files"