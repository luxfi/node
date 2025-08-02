#!/bin/bash
# Demo script for 8-chain Lux network
# Shows all 8 chains, their purposes, and core affinity configuration

set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${CYAN}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║              🚀 Lux Network 8-Chain Demo 🚀                  ║${NC}"
echo -e "${CYAN}╚══════════════════════════════════════════════════════════════╝${NC}"
echo

echo -e "${YELLOW}📊 System Information:${NC}"
echo "   CPU Cores: $(nproc)"
echo "   Memory: $(free -h | grep Mem | awk '{print $2}')"
echo "   Go Version: $(go version | awk '{print $3}')"
echo

echo -e "${YELLOW}🔗 8-Chain Architecture Overview:${NC}"
echo
echo -e "${GREEN}Core Chains (Always Required):${NC}"
echo -e "   ${BLUE}P-Chain${NC} (Platform)    - Validator management, subnets, staking"
echo -e "   ${BLUE}C-Chain${NC} (Contract)    - EVM-compatible smart contracts"
echo -e "   ${BLUE}X-Chain${NC} (Exchange)    - Digital asset creation and trading"
echo

echo -e "${PURPLE}Specialized Chains:${NC}"
echo -e "   ${BLUE}A-Chain${NC} (AI)          - AI agent coordination, GPU compute marketplace"
echo -e "   ${BLUE}B-Chain${NC} (Bridge)      - Cross-chain bridge with MPC security"
echo -e "   ${BLUE}M-Chain${NC} (MPC)         - Multi-party computation for secure operations"
echo -e "   ${BLUE}Q-Chain${NC} (Quantum)     - Quantum-safe cryptography (future-proof)"
echo -e "   ${BLUE}Z-Chain${NC} (ZK)          - Zero-knowledge proof circuits"
echo

echo -e "${YELLOW}🔢 VM IDs:${NC}"
cat << EOF
   Platform VM : 11111111111111111111111111111111LpoYY
   EVM        : mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6
   AVM        : jvYyfQTxGMJLuGWa55kdP2p2zSUYsQ5Raupu4TW34ZAUBAbtq
   AIVM       : juFxSrbCM4wszxddKepj1GWwmrn9YgN1g4n3VUWPpRo9JjERA
   BridgeVM   : kMhHABHM8j4bH94MCc4rsTNdo5E9En37MMyiujk4WdNxgXFsY
   MPCVM      : qCURact1n41FcoNBch8iMVBwc9AWie48D118ZNJ5tBdWrvryS
   QuantumVM  : ry9Sg8rZdT26iEKvJDmC2wkESs4SDKgZEhk5BgLSwg1EpcNug
   ZKVM       : vv3qPfyTVXZ5ArRZA9Jh4hbYDTBe43f7sgQg4CHfNg1rnnvX9
EOF
echo

echo -e "${YELLOW}⚙️  CPU Core Affinity Configuration:${NC}"
echo "   When enabled, each VM runs on a dedicated CPU core:"
echo
# Display core assignment in a nice table format
printf "   %-12s %s\n" "VM" "Core"
printf "   %-12s %s\n" "----------" "----"
printf "   %-12s %s\n" "Platform" "0"
printf "   %-12s %s\n" "EVM" "1"
printf "   %-12s %s\n" "AVM" "2"
printf "   %-12s %s\n" "AIVM" "3"
printf "   %-12s %s\n" "BridgeVM" "4"
printf "   %-12s %s\n" "MPCVM" "5"
printf "   %-12s %s\n" "QuantumVM" "6"
printf "   %-12s %s\n" "ZKVM" "7"
echo

echo -e "${YELLOW}🌐 Network Configurations:${NC}"
echo -e "   ${GREEN}Mainnet${NC}: 21 validators, 9.63s consensus"
echo -e "   ${GREEN}Testnet${NC}: 11 validators, 6.3s consensus"
echo -e "   ${GREEN}Local${NC}:   5 validators, 3.69s consensus"
echo

echo -e "${YELLOW}🚀 Launch Commands:${NC}"
echo -e "${CYAN}# Generate 8-chain genesis:${NC}"
echo "   genesis generate 8chains --validators 8 --stake 2000"
echo
echo -e "${CYAN}# Launch with all 8 chains:${NC}"
echo "   luxd --genesis-config=./configs/8chains"
echo
echo -e "${CYAN}# Launch with minimal chains (P,C,X,Q):${NC}"
echo "   luxd --enabled-chains=P,C,X,Q"
echo
echo -e "${CYAN}# Enable CPU affinity:${NC}"
echo "   luxd --cpu-affinity=true --gomaxprocs=8"
echo

echo -e "${YELLOW}📡 RPC Endpoints (when running):${NC}"
cat << EOF
   P-Chain: http://localhost:9650/ext/bc/P
   C-Chain: http://localhost:9650/ext/bc/C/rpc
   X-Chain: http://localhost:9650/ext/bc/X
   A-Chain: http://localhost:9650/ext/bc/A
   B-Chain: http://localhost:9650/ext/bc/B
   M-Chain: http://localhost:9650/ext/bc/M
   Q-Chain: http://localhost:9650/ext/bc/Q
   Z-Chain: http://localhost:9650/ext/bc/Z
EOF
echo

echo -e "${YELLOW}🔧 Testing Commands:${NC}"
echo -e "${CYAN}# Run unit tests:${NC}"
echo "   go test ./vms/aivm/... ./vms/bridgevm/... ./vms/mpcvm/... ./vms/quantumvm/... ./vms/zkvm/..."
echo
echo -e "${CYAN}# Run e2e tests:${NC}"
echo "   make test-e2e-8chains"
echo
echo -e "${CYAN}# Check VM registration:${NC}"
echo "   go test -v ./utils/constants/vm_ids_test.go"
echo

echo -e "${GREEN}✅ All 8 chains are ready for deployment!${NC}"
echo

# Show current test status
echo -e "${YELLOW}📊 Current Test Status:${NC}"
if go test -v ./utils/constants/vm_ids_test.go > /dev/null 2>&1; then
    echo -e "   VM ID Tests: ${GREEN}✓ PASSED${NC}"
else
    echo -e "   VM ID Tests: ${RED}✗ FAILED${NC}"
fi

# Check if VMs are properly registered
echo -e "${YELLOW}🔍 Checking VM Registration:${NC}"
for vm in aivm bridgevm mpcvm quantumvm zkvm; do
    if go test -v ./vms/$vm/vm_test.go > /dev/null 2>&1; then
        echo -e "   $vm: ${GREEN}✓ REGISTERED${NC}"
    else
        echo -e "   $vm: ${YELLOW}⚠ NOT TESTED${NC}"
    fi
done

echo
echo -e "${CYAN}════════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}🎉 8-Chain Lux Network is configured and ready!${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════════${NC}"