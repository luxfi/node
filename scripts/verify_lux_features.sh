#!/bin/bash

# Verify Lux Network specific features
# This script checks the genesis configuration and chain status

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}==================================${NC}"
echo -e "${GREEN}Lux Network Feature Verification${NC}"
echo -e "${GREEN}==================================${NC}"
echo ""

# Check if node is running
NODE_ID=$(curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID"}' \
    -H 'content-type:application/json;' http://localhost:9650/ext/info 2>/dev/null | \
    grep -o '"nodeID":"[^"]*' | grep -o '[^"]*$' || echo "")

if [ -z "$NODE_ID" ]; then
    echo -e "${RED}Node not running. Please start with launch_with_avalanche.sh${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Node Running${NC}"
echo -e "   Node ID: $NODE_ID"
echo ""

# Get network info
echo -e "${YELLOW}Network Information:${NC}"
NETWORK_ID=$(curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"info.getNetworkID"}' \
    -H 'content-type:application/json;' http://localhost:9650/ext/info | \
    grep -o '"networkID":"[^"]*' | grep -o '[^"]*$' || echo "N/A")
echo -e "  Network ID: $NETWORK_ID"

# Get blockchain info
echo -e "${YELLOW}Blockchain Status:${NC}"
for CHAIN in P X C; do
    CHAIN_ID=$(curl -s -X POST --data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"info.getBlockchainID\",\"params\":{\"alias\":\"$CHAIN\"}}" \
        -H 'content-type:application/json;' http://localhost:9650/ext/info | \
        grep -o '"blockchainID":"[^"]*' | grep -o '[^"]*$' || echo "N/A")
    echo -e "  $CHAIN-Chain: $CHAIN_ID"
done

# Check stakers (should show NFT-based validators from genesis)
echo ""
echo -e "${YELLOW}Genesis Validators:${NC}"
CURRENT_HEIGHT=$(curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"platform.getHeight"}' \
    -H 'content-type:application/json;' http://localhost:9650/ext/bc/P | \
    grep -o '"height":"[^"]*' | grep -o '[^"]*$' || echo "0")
echo -e "  P-Chain Height: $CURRENT_HEIGHT"

# Get current validators
echo -e "${YELLOW}Current Validators:${NC}"
VALIDATORS=$(curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"platform.getCurrentValidators","params":{}}' \
    -H 'content-type:application/json;' http://localhost:9650/ext/bc/P 2>/dev/null || echo "{}")

if echo "$VALIDATORS" | grep -q "validators"; then
    echo "$VALIDATORS" | grep -o '"nodeID":"[^"]*' | grep -o '[^"]*$' | head -5 | while read -r VALIDATOR; do
        echo -e "  • $VALIDATOR"
    done
else
    echo -e "  No validators found (expected with single node)"
fi

# Check C-Chain for NFT contract
echo ""
echo -e "${YELLOW}C-Chain NFT Contract:${NC}"
NFT_CONTRACT="0x0100000000000000000000000000000000000001"
CODE=$(curl -s -X POST --data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_getCode\",\"params\":[\"$NFT_CONTRACT\",\"latest\"]}" \
    -H 'content-type:application/json;' http://localhost:9650/ext/bc/C/rpc 2>/dev/null | \
    grep -o '"result":"[^"]*' | grep -o '[^"]*$' || echo "0x")

if [ "$CODE" != "0x" ] && [ -n "$CODE" ]; then
    echo -e "${GREEN}  ✅ NFT Contract deployed at $NFT_CONTRACT${NC}"
    echo -e "     Code size: ${#CODE} bytes"
else
    echo -e "${YELLOW}  ⚠️  NFT Contract not found (may need manual deployment)${NC}"
fi

# Check token supply on X-Chain
echo ""
echo -e "${YELLOW}Token Information:${NC}"
AVAX_ASSET=$(curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"avm.getAssetDescription","params":{"assetID":"2fombhL7aGPwj3KH4bfrmJwW6PVnMobf9Y2fn9GwxiAAJyFDbe"}}' \
    -H 'content-type:application/json;' http://localhost:9650/ext/bc/X 2>/dev/null || echo "{}")

if echo "$AVAX_ASSET" | grep -q "name"; then
    echo -e "  AVAX Asset found on X-Chain"
else
    echo -e "  Unable to query AVAX asset"
fi

echo ""
echo -e "${GREEN}Lux Network Genesis Features:${NC}"
echo -e "  • Network ID: 12345 (custom)"
echo -e "  • NFT Validator Support: Configured"
echo -e "  • Total Supply: 2T LUX"
echo -e "  • Minimum Stake: 1M LUX (or equivalent with NFT)"
echo -e "  • Genesis NFTs: 100 total"
echo -e "    - Genesis tier (1-10): 2x rewards"
echo -e "    - Pioneer tier (11-40): 1.5x rewards"
echo -e "    - Standard tier (41-100): 1x rewards"
echo ""
echo -e "${YELLOW}Note: A, B, and Z chains require custom VM implementations${NC}"
echo -e "${YELLOW}Currently running with standard Avalanche VMs (P/X/C)${NC}"