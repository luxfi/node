#!/bin/bash

echo "Fixing all malformed imports in Go files..."

# Fix metric metrics pattern
find . -name "*.go" -type f -exec grep -l 'metric metrics "github.com/luxfi/metric"' {} \; | while read -r file; do
    echo "Fixing metric import in: $file"
    sed -i '' 's/metric metrics "github\.com\/luxfi\/metric"/"github.com\/luxfi\/metric"/g' "$file"
done

# Fix prometheus metrics pattern
find . -name "*.go" -type f -exec grep -l 'prometheus metrics "github.com/prometheus' {} \; | while read -r file; do
    echo "Fixing prometheus import in: $file"
    sed -i '' 's/prometheus metrics "github\.com\/prometheus/"github.com\/prometheus/g' "$file"
done

# Fix avalabs node pattern
find . -name "*.go" -type f -exec grep -l 'avalabs node "github.com/ava-labs' {} \; | while read -r file; do
    echo "Fixing avalabs import in: $file"
    sed -i '' 's/avalabs node "github\.com\/ava-labs/"github.com\/luxfi/g' "$file"
done

# Fix consensus errors pattern  
find . -name "*.go" -type f -exec grep -l 'consensus errors "github.com/luxfi/consensus' {} \; | while read -r file; do
    echo "Fixing consensus import in: $file"
    sed -i '' 's/consensus errors "github\.com\/luxfi\/consensus/"github.com\/luxfi\/consensus/g' "$file"
done

# Fix any double-word import patterns (generic)
find . -name "*.go" -type f | while read -r file; do
    # Match pattern: word word "import/path"
    if grep -E '^\s+[a-z]+ [a-z]+ "' "$file" > /dev/null 2>&1; then
        echo "Checking for double-word imports in: $file"
        # Remove the first word from double-word imports
        sed -i '' -E 's/^([[:space:]]+)[a-z]+ ([a-z]+) ("/\1"\3/g' "$file"
    fi
done

echo "Import fixes complete!"

# Verify no more import errors
echo ""
echo "Checking for remaining import errors..."
go build ./... 2>&1 | grep "import path must be a string" | head -5

if [ $? -eq 0 ]; then
    echo "Some import errors still remain. Please check manually."
else
    echo "All import errors appear to be fixed!"
fi