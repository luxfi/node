#!/bin/bash

# POA Demo Script - Shows how to use POA mode

echo "==================================="
echo "LUX Node POA (Proof of Authority) Mode Demo"
echo "==================================="
echo ""
echo "POA mode allows running a blockchain with a single node without"
echo "validator staking requirements. This is useful for:"
echo "- Development and testing"
echo "- Private networks"
echo "- Consortium blockchains"
echo ""
echo "Key Features:"
echo "- K=1, Alpha=1, Beta=1 consensus parameters"
echo "- Single node can produce and finalize blocks"
echo "- No staking requirements"
echo "- Configurable block time"
echo ""
echo "To run LUX node in POA mode:"
echo ""
echo "./build/luxd \\"
echo "  --poa-mode-enabled \\"
echo "  --poa-single-node-mode \\"
echo "  --poa-min-block-time=1s \\"
echo "  --sybil-protection-disabled \\"
echo "  --sybil-protection-disabled-weight=1000000 \\"
echo "  --network-id=96369"
echo ""
echo "Additional options:"
echo "  --poa-authorized-nodes=<node1,node2>  # For multi-node POA"
echo ""
echo "Configuration can also be set in config file:"
echo '{'
echo '  "poa-mode-enabled": true,'
echo '  "poa-single-node-mode": true,'
echo '  "poa-min-block-time": "1s",'
echo '  "sybil-protection-disabled": true,'
echo '  "sybil-protection-disabled-weight": 1000000'
echo '}'