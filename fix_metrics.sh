#!/bin/bash

# Fix all metrics registration issues by casting to prometheus.Collector
# This is a temporary fix until we can refactor the metrics package properly

find . -name "*.go" -type f | while read -r file; do
  # Skip test files and vendor
  if [[ "$file" == *"_test.go" ]] || [[ "$file" == *"/vendor/"* ]]; then
    continue
  fi

  # Check if file contains registerer.Register or reg.Register patterns
  if grep -q "\.Register(" "$file"; then
    # Add prometheus import if needed and not already present
    if ! grep -q "github.com/prometheus/client_golang/prometheus" "$file"; then
      if grep -q "^import (" "$file"; then
        # Multi-line import
        sed -i '/^import (/a\\t"github.com/prometheus/client_golang/prometheus"' "$file"
      elif grep -q "^import " "$file"; then
        # Single import - convert to multi
        sed -i 's/^import .*/&\n\nimport (\n\t"github.com/prometheus/client_golang/prometheus"\n)/' "$file"
      fi
    fi

    # Fix registerer.Register calls that pass metrics interfaces
    # This regex matches patterns like registerer.Register(m.someMetric)
    sed -i 's/\(registerer\.Register(\)\(m\.[a-zA-Z]*\)\()\)/\1\2.(prometheus.Collector)\3/g' "$file"
    sed -i 's/\(reg\.Register(\)\(m\.[a-zA-Z]*\)\()\)/\1\2.(prometheus.Collector)\3/g' "$file"
    sed -i 's/\(registerer\.Register(\)\(metrics\.[a-zA-Z]*\)\()\)/\1\2.(prometheus.Collector)\3/g' "$file"
    sed -i 's/\(reg\.Register(\)\(a\.[a-zA-Z]*\)\()\)/\1\2.(prometheus.Collector)\3/g' "$file"
    sed -i 's/\(reg\.Register(\)\(tm\.[a-zA-Z]*\)\()\)/\1\2.(prometheus.Collector)\3/g' "$file"
  fi
done

echo "Metrics fixes applied"