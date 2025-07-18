#!/bin/bash
# Script to analyze consensus dependencies

echo "=== Analyzing consensus package dependencies ==="

echo -e "\n1. Files importing snowball:"
grep -r "snow/consensus/snowball" --include="*.go" . | grep -v "_test.go" | cut -d: -f1 | sort -u | wc -l

echo -e "\n2. Files importing snowman:"
grep -r "snow/consensus/snowman" --include="*.go" . | grep -v "_test.go" | cut -d: -f1 | sort -u | wc -l

echo -e "\n3. Files importing snowstorm:"
grep -r "snow/consensus/snowstorm" --include="*.go" . | grep -v "_test.go" | cut -d: -f1 | sort -u | wc -l

echo -e "\n4. Files importing lux consensus:"
grep -r "snow/consensus/lux" --include="*.go" . | grep -v "_test.go" | cut -d: -f1 | sort -u | wc -l

echo -e "\n5. Files importing snow/choices:"
grep -r "snow/choices" --include="*.go" . | grep -v "_test.go" | cut -d: -f1 | sort -u | wc -l

echo -e "\n=== Package cross-dependencies ==="
echo -e "\nSnowball imports:"
grep -r "^import\|github.com/luxfi/node" snow/consensus/snowball/*.go | grep -v test | grep -v "snowball"

echo -e "\nSnowman imports:"
grep -r "github.com/luxfi/node/snow/consensus" snow/consensus/snowman/*.go | grep -v test | head -10