#!/bin/bash
# Test C-Chain functionality

RPC_URL="http://127.0.0.1:9630/ext/bc/C/rpc"
TEST_ACCOUNT="0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"

echo "Testing LUX C-Chain RPC endpoint..."
echo "================================="

# Test 1: Chain ID
echo -n "Chain ID: "
curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId"}' \
     -H 'content-type:application/json' $RPC_URL | jq -r '.result' | xargs printf "%d\n"

# Test 2: Block Number
echo -n "Current Block: "
curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}' \
     -H 'content-type:application/json' $RPC_URL | jq -r '.result' | xargs printf "%d\n"

# Test 3: Account Balance
echo -n "Test Account Balance: "
BALANCE=$(curl -s -X POST --data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_getBalance\",\"params\":[\"$TEST_ACCOUNT\",\"latest\"]}" \
               -H 'content-type:application/json' $RPC_URL | jq -r '.result')
# Convert hex to decimal and format
echo "$BALANCE" | xargs printf "%d LUX (wei)\n"

# Test 4: Network Version
echo -n "Network Version: "
curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"net_version"}' \
     -H 'content-type:application/json' $RPC_URL | jq -r '.result'

# Test 5: Gas Price
echo -n "Gas Price: "
curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"eth_gasPrice"}' \
     -H 'content-type:application/json' $RPC_URL | jq -r '.result' | xargs printf "%d wei\n"

echo "================================="
echo "C-Chain RPC is operational!"