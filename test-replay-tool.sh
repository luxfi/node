#!/bin/bash

echo "=== C-Chain Replay Tool Test ==="
echo
echo "This tool supports both BadgerDB and PebbleDB formats for replaying blockchain data."
echo "It auto-detects the database format and uses the appropriate backend."
echo
echo "Features:"
echo "  - Auto-detects database backend (BadgerDB or PebbleDB)"
echo "  - Supports namespaced databases (SubnetEVM format)"
echo "  - Executes transactions through EVM for proper state building"
echo "  - Tracks wallet balances during replay"
echo "  - Verifies state roots if requested"
echo
echo "Usage examples:"
echo
echo "1. Replay from BadgerDB (auto-detect):"
echo "   ./bin/cchain-replay -source /home/z/.luxd-node1/chainData/C/db -target /tmp/cchain-db"
echo
echo "2. Replay from PebbleDB with verification:"
echo "   ./bin/cchain-replay -source /path/to/pebbledb -target /tmp/cchain-db -backend pebbledb -verify"
echo
echo "3. Test mode with wallet tracking:"
echo "   ./bin/cchain-replay -source /path/to/source -target /tmp/test-db -test -wallet 0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"
echo
echo "Current implementation:"
echo "  - Database backends: BadgerDB ✓, PebbleDB ✓"
echo "  - Auto-detection: Checks for .vlog files (BadgerDB) or CURRENT file (PebbleDB)"
echo "  - Default backend: BadgerDB (if auto-detection finds .sst files)"
echo
echo "Note: Source databases must not be in use (locked) when running the replay tool."
echo
echo "Binary location: /home/z/work/lux/node/bin/cchain-replay"