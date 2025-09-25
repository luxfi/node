#!/bin/bash

echo "=== Fixing prometheus registry references ==="

# Replace NewPrometheusRegistry with NewRegistry
find . -name "*.go" -type f | while read file; do
    if grep -q "metric\.NewPrometheusRegistry" "$file" 2>/dev/null; then
        echo "  Fixing $file"
        sed -i 's/metric\.NewPrometheusRegistry()/metric.NewRegistry()/g' "$file"
    fi
    if grep -q "luxmetrics\.NewPrometheusRegistry" "$file" 2>/dev/null; then
        echo "  Fixing $file"
        sed -i 's/luxmetrics\.NewPrometheusRegistry()/luxmetrics.NewRegistry()/g' "$file"
    fi
done

# Also check for NewNoOpRegistry usage that should be NewRegistry
find . -name "*.go" -type f | while read file; do
    if grep -q "metric\.NewNoOpRegistry" "$file" 2>/dev/null; then
        echo "  Converting NewNoOpRegistry to NewRegistry in $file"
        sed -i 's/metric\.NewNoOpRegistry()/metric.NewRegistry()/g' "$file"
    fi
done

echo "=== Done fixing prometheus registry references ==="
