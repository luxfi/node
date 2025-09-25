#!/bin/bash

echo "Fixing remaining metric system issues..."

# Fix any remaining luxmetric vs luxmetrics issues
find . -name "*.go" -exec grep -l "luxmetric\." {} \; | while read file; do
    echo "Fixing $file"
    sed -i 's/\bluxmetric\./luxmetrics./g' "$file"
done

# Fix undefined metrics references
find . -name "*.go" -exec grep -l "undefined: metrics" {} \; | while read file; do
    echo "Fixing undefined metrics in $file"
    sed -i 's/\bmetrics\./metric./g' "$file"
done

# Fix NewNoOpMetrics usage
find . -name "*.go" -exec grep -l "NewNoOpMetrics" {} \; | while read file; do
    echo "Fixing NewNoOpMetrics in $file"
    sed -i 's/metric\.NewNoOpMetrics("[^"]*")/metric.NewNoOp()/g' "$file"
    sed -i 's/metrics\.NewNoOpMetrics("[^"]*")/metric.NewNoOp()/g' "$file"
done

# Fix undefined prometheus references
find . -name "*.go" -exec grep -l "undefined: prometheus" {} \; | while read file; do
    echo "Fixing prometheus in $file"
    sed -i 's/prometheus\.NewRegistry()/metric.NewNoOpRegistry()/g' "$file"
done

echo "Done fixing issues!"
