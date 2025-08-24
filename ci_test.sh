#!/bin/bash
set -e

echo "=== CI Simulation Test ==="
echo "Testing critical packages that CI will test..."

# Build test
echo -e "\n[1/4] Testing Build..."
if go build -o /tmp/test_luxd ./main; then
    echo "✅ Build: SUCCESS"
else
    echo "❌ Build: FAILED"
    exit 1
fi

# Version check
echo -e "\n[2/4] Testing Version..."
VERSION=$(/tmp/test_luxd --version)
if [[ $VERSION == *"1.13.5"* ]]; then
    echo "✅ Version: $VERSION"
else
    echo "❌ Version mismatch: $VERSION"
    exit 1
fi

# Critical package tests
echo -e "\n[3/4] Testing Critical Packages..."
PACKAGES=(
    "./network/p2p/lp118"
    "./consensus/engine/common"
    "./wallet/chain/p"
    "./wallet/subnet/primary"
)

for pkg in "${PACKAGES[@]}"; do
    echo -n "Testing $pkg... "
    if go test -short -timeout 30s "$pkg" > /dev/null 2>&1; then
        echo "✅"
    else
        echo "❌"
        exit 1
    fi
done

# Example compilation test
echo -e "\n[4/4] Testing Examples Compilation..."
EXAMPLES=(
    "./wallet/subnet/primary/examples/sign-l1-validator-removal-registration"
    "./wallet/chain/p/wallet"
)

for example in "${EXAMPLES[@]}"; do
    echo -n "Building $example... "
    if go build "$example" > /dev/null 2>&1; then
        echo "✅"
    else
        echo "❌"
        exit 1
    fi
done

echo -e "\n=== CI Simulation Complete ==="
echo "✅ All tests passed - CI should pass!"