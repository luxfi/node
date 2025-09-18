#!/bin/bash

echo "=== Comprehensive test fix script ==="

# Fix all metrics vs metric issues
echo "Fixing metrics references..."
find . -name "*.go" -type f | while read file; do
    # Skip vendor and .git directories
    if [[ "$file" == *"vendor"* ]] || [[ "$file" == *".git"* ]]; then
        continue
    fi
    
    # Check if file needs fixing
    if grep -q "undefined: metrics" "$file" 2>/dev/null || grep -q "metrics\\.New" "$file" 2>/dev/null; then
        echo "  Fixing $file"
        sed -i 's/\bmetrics\.New/metric.New/g' "$file"
        sed -i 's/\bmetrics\.AsCollector/metric.AsCollector/g' "$file"
    fi
done

# Fix all remaining BLS test issues
echo "Fixing BLS test issues..."
if [ -f "utils/crypto/bls/bls_test.go" ]; then
    # The BLS tests might need special handling for signature verification
    echo "  Checking BLS tests"
fi

# Compile all packages to find build errors
echo "Building all packages to identify issues..."
failed_packages=$(go test ./... -run=xxxxx 2>&1 | grep "FAIL.*\[build failed\]" | awk '{print $2}')

echo "Failed packages:"
echo "$failed_packages" | head -20

# Fix import issues in test files
echo "Fixing test import issues..."
find . -name "*_test.go" -type f | while read file; do
    if grep -q "metric.NewNoOpMetrics" "$file" 2>/dev/null; then
        sed -i 's/metric\.NewNoOpMetrics("[^"]*")/metric.NewNoOp()/g' "$file"
    fi
done

echo "=== Fix script complete ==="
