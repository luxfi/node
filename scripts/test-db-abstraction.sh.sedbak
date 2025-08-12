#!/bin/bash

echo "Testing database abstraction layer..."

# Clean up previous test
rm -rf /tmp/luxd-db-test

# Create test directories
mkdir -p /tmp/luxd-db-test
mkdir -p /tmp/luxd-db-test/configs/chains

# Create chain configs to test different database types
mkdir -p /tmp/luxd-db-test/configs/chains/C
cat > /tmp/luxd-db-test/configs/chains/C/config.json << 'EOF'
{
  "db-type": "badgerdb",
  "log-level": "info"
}
EOF

mkdir -p /tmp/luxd-db-test/configs/chains/X
cat > /tmp/luxd-db-test/configs/chains/X/config.json << 'EOF'
{
  "db-type": "pebbledb",
  "log-level": "info"  
}
EOF

# Use the test staking keys
cp -r /home/z/work/lux/genesis/test-mainnet-keys /tmp/luxd-db-test/

echo "Starting luxd with database abstraction test..."
echo "P-Chain: pebbledb (default)"
echo "C-Chain: badgerdb (config override)"
echo "X-Chain: pebbledb (config override)"

cd /home/z/work/lux/node

# Run with local network ID which doesn't require specific validators
timeout 10 ./build/luxd \
    --network-id=local \
    --data-dir=/tmp/luxd-db-test \
    --chain-config-dir=/tmp/luxd-db-test/configs/chains \
    --db-type=pebbledb \
    --staking-tls-cert-file=/tmp/luxd-db-test/test-mainnet-keys/staker.crt \
    --staking-tls-key-file=/tmp/luxd-db-test/test-mainnet-keys/staker.key \
    --staking-signer-key-file=/tmp/luxd-db-test/test-mainnet-keys/signer.key \
    --http-host=0.0.0.0 \
    --http-port=9630 \
    --staking-port=9631 \
    --log-level=debug \
    --sybil-protection-enabled=false \
    --api-admin-enabled=true 2>&1 | tee /tmp/luxd-db-test.log

echo ""
echo "Checking database types created..."
echo ""

# Check if databases were created with correct types
for chain in P C X; do
    db_path="/tmp/luxd-db-test/db/local/${chain}"
    if [ -d "$db_path" ]; then
        echo "Chain $chain database path exists: $db_path"
        # Check for database-specific files
        if [ -f "$db_path/MANIFEST-000000" ] || [ -f "$db_path/000001.log" ]; then
            echo "  - Appears to be PebbleDB/LevelDB format"
        elif [ -d "$db_path/badger" ] || [ -f "$db_path/KEYREGISTRY" ]; then
            echo "  - Appears to be BadgerDB format"
        fi
    fi
done

echo ""
echo "Database abstraction test complete!"
echo "Check /tmp/luxd-db-test.log for details"