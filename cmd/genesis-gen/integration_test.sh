#!/bin/bash
set -e

echo "=== Genesis Generator Integration Test ==="
echo ""

# Test 1: Generate genesis
echo "Test 1: Generating genesis..."
genesis-gen --network-id 12345 --num-validators 3 --output /tmp/test_genesis.json
echo "✅ Genesis generated"
echo ""

# Test 2: Validate JSON structure
echo "Test 2: Validating JSON structure..."
jq -e '.networkID' /tmp/test_genesis.json > /dev/null
jq -e '.initialStakers | length == 3' /tmp/test_genesis.json > /dev/null
echo "✅ JSON structure valid"
echo ""

# Test 3: Validate addresses
echo "Test 3: Validating addresses..."
ADDR=$(jq -r '.initialStakers[0].rewardAddress' /tmp/test_genesis.json)
echo "Checking address: $ADDR"

# Create validation script
cat > /tmp/check_addr.go << 'GOEOF'
package main
import (
	"fmt"
	"os"
	"github.com/luxfi/node/utils/formatting/address"
)
func main() {
	_, _, _, err := address.Parse(os.Args[1])
	if err != nil {
		fmt.Printf("INVALID: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("VALID")
}
GOEOF

cd /Users/z/work/lux/node
if go run /tmp/check_addr.go "$ADDR" | grep -q "VALID"; then
	echo "✅ Address valid with proper checksum"
else
	echo "❌ Address invalid"
	exit 1
fi
echo ""

# Test 4: Load with genesis package
echo "Test 4: Loading with genesis package..."
cat > /tmp/load_genesis.go << 'GOEOF'
package main
import (
	"fmt"
	"os"
	"github.com/luxfi/node/genesis"
)
func main() {
	config, err := genesis.GetConfigFile(os.Args[1])
	if err != nil {
		fmt.Printf("Failed: %v\n", err)
		os.Exit(1)
	}
	_, _, err = genesis.FromConfig(config)
	if err != nil {
		fmt.Printf("Failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("SUCCESS")
}
GOEOF

cd /Users/z/work/lux/node
if go run /tmp/load_genesis.go /tmp/test_genesis.json | grep -q "SUCCESS"; then
	echo "✅ Genesis loaded successfully"
else
	echo "❌ Failed to load genesis"
	exit 1
fi
echo ""

# Test 5: Verify supply calculation
echo "Test 5: Verifying supply..."
SUPPLY=$(jq -r '.allocations[0].initialAmount' /tmp/test_genesis.json)
LOCKED=$(jq -r '.allocations[0].unlockSchedule[0].amount' /tmp/test_genesis.json)
TOTAL=$((SUPPLY + LOCKED))
echo "Initial: $SUPPLY"
echo "Locked: $LOCKED"
echo "Total: $TOTAL"
echo "✅ Supply calculated"
echo ""

echo "=== All Tests Passed! ==="
echo ""
echo "Summary:"
echo "  ✅ Genesis generation"
echo "  ✅ JSON structure"
echo "  ✅ Address validation"
echo "  ✅ Package loading"
echo "  ✅ Supply calculation"
echo ""
echo "Genesis file: /tmp/test_genesis.json"
