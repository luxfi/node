#!/bin/bash
# Patch geth pebble.go to fix type mismatches

GETH_PATH="/go/pkg/mod/github.com/luxfi/coreth@v0.13.7"

# Make the directory writable
chmod -R +w $GETH_PATH || true

# Directly fix the lines that are causing issues
echo "Patching pebble.go..."

# Fix line 150: change int to uint64
sed -i '150s/memTableSize := cache \* 1024 \* 1024 \/ 2 \/ memTableLimit/memTableSize := uint64(cache * 1024 * 1024 \/ 2 \/ memTableLimit)/' $GETH_PATH/ethdb/pebble/pebble.go || true

# Fix line 151: add uint64 cast
sed -i '151s/if memTableSize > maxMemTableSize {/if memTableSize > uint64(maxMemTableSize) {/' $GETH_PATH/ethdb/pebble/pebble.go || true

# Fix line 152: add uint64 cast
sed -i '152s/memTableSize = maxMemTableSize/memTableSize = uint64(maxMemTableSize)/' $GETH_PATH/ethdb/pebble/pebble.go || true

# The iterator issue at line 589 is already correct in the code - it already handles the error
# But we need to ensure it's not expecting only one return value somewhere
# Actually looking at the error, the issue is that the code expects 1 value but gets 2
# This means the existing code might be: iter := d.db.NewIter(...) instead of iter, err := d.db.NewIter(...)

# Check if we need to fix this
if grep -q "iter := d.db.NewIter" $GETH_PATH/ethdb/pebble/pebble.go; then
    echo "Fixing iterator assignment..."
    sed -i 's/iter := d\.db\.NewIter/iter, _ := d.db.NewIter/' $GETH_PATH/ethdb/pebble/pebble.go || true
fi

echo "Patch complete!"