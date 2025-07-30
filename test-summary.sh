#!/bin/bash
# Run a summary of tests across key packages

echo "====================================="
echo "Running Lux Node Test Summary"
echo "====================================="
echo ""

# Test key packages that don't require network access
packages=(
    "./version"
    "./api/health"
    "./database/memdb"
    "./database/leveldb"
    "./database/pebbledb"
    "./utils/json"
    "./utils/crypto/secp256k1"
    "./utils/formatting"
    "./ids"
)

total=0
passed=0
failed=0

for pkg in "${packages[@]}"; do
    echo -n "Testing $pkg ... "
    if go test "$pkg" -short -timeout 30s > /dev/null 2>&1; then
        echo "✅ PASS"
        ((passed++))
    else
        echo "❌ FAIL"
        ((failed++))
    fi
    ((total++))
done

echo ""
echo "====================================="
echo "Test Summary:"
echo "Total packages tested: $total"
echo "Passed: $passed"
echo "Failed: $failed"
echo "====================================="

if [ $failed -eq 0 ]; then
    echo "✅ All tests passed!"
    exit 0
else
    echo "❌ Some tests failed"
    exit 1
fi