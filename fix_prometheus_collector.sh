#!/bin/bash
# Remove prometheus.Collector type assertions

find . -name "*.go" -type f | while read file; do
    # Skip vendor and .git directories
    if [[ "$file" == *"vendor"* ]] || [[ "$file" == *".git"* ]]; then
        continue
    fi
    
    # Remove .(prometheus.Collector) type assertions
    sed -i 's/\.\(prometheus\.Collector\)//g' "$file"
done
echo "Removed all prometheus.Collector type assertions"
