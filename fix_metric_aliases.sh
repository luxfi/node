#!/bin/bash

echo "Fixing metric import aliases..."

# Find all files with aliased imports
find . -name "*.go" -type f | while read -r file; do
    # Check if file has the alias
    if grep -q 'metrics "github.com/luxfi/metric"' "$file" 2>/dev/null; then
        echo "Processing $file"

        # Replace the import line
        sed -i 's|metrics "github.com/luxfi/metric"|"github.com/luxfi/metric"|' "$file"

        # Replace all metrics. with metric.
        sed -i 's/metrics\./metric\./g' "$file"

        echo "  Fixed $file"
    fi

    # Also check for luxmetrics alias
    if grep -q 'luxmetrics "github.com/luxfi/metric"' "$file" 2>/dev/null; then
        echo "Processing $file"

        # Replace the import line
        sed -i 's|luxmetrics "github.com/luxfi/metric"|"github.com/luxfi/metric"|' "$file"

        # Replace all luxmetrics. with metric.
        sed -i 's/luxmetrics\./metric\./g' "$file"

        echo "  Fixed $file"
    fi
done

echo "Complete!"