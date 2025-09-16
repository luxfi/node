#!/bin/bash

# Fix all malformed imports in the codebase

echo "Finding and fixing malformed imports..."

# Find all Go files with potential import issues
find . -name "*.go" -type f | while read -r file; do
    # Check for malformed imports like "luxmetric metrics"
    if grep -E 'luxmetric\s+metrics\s+"github\.com/luxfi/metric"' "$file" > /dev/null 2>&1; then
        echo "Fixing import in: $file"
        sed -i '' 's/luxmetric metrics "github\.com\/luxfi\/metric"/"github.com\/luxfi\/metric"/g' "$file"
    fi
    
    # Check for other malformed imports with extra identifiers
    if grep -E '\s+[a-z]+\s+[a-z]+\s+"' "$file" > /dev/null 2>&1; then
        echo "Checking potential import issue in: $file"
        # Fix common patterns
        sed -i '' 's/prometheus metrics "github\.com\/prometheus\/client_golang\/prometheus"/"github.com\/prometheus\/client_golang\/prometheus"/g' "$file"
        sed -i '' 's/avalabs node "github\.com\/ava-labs\/avalanchego"/"github.com\/luxfi\/node"/g' "$file"
        sed -i '' 's/luxfi metrics "github\.com\/luxfi\/metric"/"github.com\/luxfi\/metric"/g' "$file"
    fi
done

echo "Import fixes complete!"