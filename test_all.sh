#!/bin/bash

echo "Running comprehensive test suite..."
echo "=================================="

PASS=0
FAIL=0
SKIP=0

# Test core packages
echo "Testing core packages..."
for pkg in ids utils codec cache consensus database metric log crypto trace; do
    echo -n "Testing $pkg... "
    if go test -count=1 -timeout=30s ./.../$pkg/... > /dev/null 2>&1; then
        echo "✅ PASS"
        ((PASS++))
    else
        echo "❌ FAIL"
        ((FAIL++))
    fi
done

# Test VM packages
echo ""
echo "Testing VM packages..."
for pkg in vms/components vms/secp256k1fx vms/platformvm/api vms/platformvm/state vms/platformvm/txs; do
    echo -n "Testing $pkg... "
    if go test -count=1 -timeout=30s ./$pkg/... > /dev/null 2>&1; then
        echo "✅ PASS"
        ((PASS++))
    else
        echo "❌ FAIL"
        ((FAIL++))
    fi
done

# Test network packages
echo ""
echo "Testing network packages..."
for pkg in network message nat network/p2p network/peer network/throttling; do
    echo -n "Testing $pkg... "
    if go test -count=1 -timeout=30s ./$pkg/... > /dev/null 2>&1; then
        echo "✅ PASS"
        ((PASS++))
    else
        echo "❌ FAIL"
        ((FAIL++))
    fi
done

echo ""
echo "=================================="
echo "Test Summary:"
echo "  PASSED: $PASS"
echo "  FAILED: $FAIL"
echo "  TOTAL:  $((PASS + FAIL))"
echo "  Pass Rate: $(( PASS * 100 / (PASS + FAIL) ))%"
echo "=================================="