#!/bin/bash

echo "Fixing all luxmetric references to metric..."

# Replace all luxmetric. with metric.
find . -name "*.go" -type f | while read -r file; do
    if grep -q "luxmetric\\." "$file" 2>/dev/null; then
        echo "Fixing $file"
        sed -i 's/luxmetric\./metric\./g' "$file"
    fi
done

echo "Complete!"