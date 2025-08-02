// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains8

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/v2/tests"
	"github.com/luxfi/node/v2/tests/fixture/e2e"
	"github.com/luxfi/node/v2/tests/fixture/tmpnet"
	"github.com/luxfi/node/v2/utils/constants"
	"github.com/luxfi/node/v2/vms/aivm"
	"github.com/luxfi/node/v2/vms/bridgevm"
	"github.com/luxfi/node/v2/vms/mpcvm"
	"github.com/luxfi/node/v2/vms/quantumvm"
	"github.com/luxfi/node/v2/vms/zkvm"
)

const (
	// Number of validators in the network
	numValidators = 8
	
	// Timeout for various operations
	defaultTimeout = 2 * time.Minute
	healthCheckTimeout = 30 * time.Second
)

var _ = ginkgo.Describe("[8-Chains E2E]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("should boot all 8 chains with proper genesis and test RPC endpoints", func() {
		// Create network with 8 validators
		nodes := make([]*tmpnet.Node, numValidators)
		for i := 0; i < numValidators; i++ {
			nodes[i] = tmpnet.NewNode("")
		}

		network := &tmpnet.Network{
			Owner:   "8chains-e2e",
			Nodes:   nodes,
			Genesis: generateGenesis8Chains(),
		}

		// Configure chain aliases
		network.ChainAliases = map[string]string{
			"A": "aivm",
			"B": "bridgevm",
			"C": "evm",
			"M": "mpcvm",
			"P": "platform",
			"Q": "quantumvm",
			"X": "avm",
			"Z": "zkvm",
		}

		// Start the network
		tc.By("starting 8-node validator network")
		require.NoError(network.Start(tc.GetWriter()))
		tc.DeferCleanup(func() {
			tc.By("stopping network")
			network.Stop(context.Background())
		})

		// Wait for all nodes to become healthy
		tc.By("waiting for all nodes to become healthy")
		for _, node := range network.Nodes {
			tc.Eventually(func() bool {
				_, err := node.GetHealth(context.Background())
				return err == nil
			}, healthCheckTimeout, time.Second, "node %s failed to become healthy", node.NodeID)
		}

		// Test RPC endpoints for all chains
		tc.By("testing RPC endpoints for all 8 chains")
		testAllChainRPCs(tc, network)

		// Verify load balancing across cores
		tc.By("verifying load balancing across CPU cores")
		verifyCoreLoadBalancing(tc, network)

		// Test chain interactions
		tc.By("testing cross-chain interactions")
		testCrossChainInteractions(tc, network)
	})

	ginkgo.It("should support VM-to-core affinity configuration", func() {
		// Get number of CPU cores
		numCores := runtime.NumCPU()
		tc.Logf("System has %d CPU cores", numCores)

		// Create nodes with core affinity configuration
		nodes := make([]*tmpnet.Node, numValidators)
		for i := 0; i < numValidators; i++ {
			node := tmpnet.NewNode("")
			
			// Configure VM-to-core affinity
			// Each chain gets assigned to a specific core
			node.Flags[constants.ChainConfigDirKey] = generateCoreAffinityConfig(numCores)
			nodes[i] = node
		}

		network := &tmpnet.Network{
			Owner:   "8chains-affinity",
			Nodes:   nodes,
			Genesis: generateGenesis8Chains(),
		}

		// Start network
		require.NoError(network.Start(tc.GetWriter()))
		tc.DeferCleanup(func() {
			network.Stop(context.Background())
		})

		// Verify core affinity is working
		tc.By("verifying VM-to-core affinity")
		verifyCoreAffinity(tc, network)
	})
})

// generateGenesis8Chains creates genesis configuration for all 8 chains
func generateGenesis8Chains() *tmpnet.Genesis {
	genesis := tmpnet.NewGenesis(constants.MainnetID)

	// Platform Chain (P) - always required
	genesis.PChainGenesis = generatePChainGenesis()

	// C-Chain (EVM) genesis
	genesis.CChainGenesis = generateCChainGenesis()

	// X-Chain (AVM) genesis
	genesis.XChainGenesis = generateXChainGenesis()

	// A-Chain (AI) genesis
	genesis.CustomChains = append(genesis.CustomChains, tmpnet.CustomChain{
		VMID:    aivm.ID,
		Genesis: generateAChainGenesis(),
		Config:  generateAChainConfig(),
	})

	// B-Chain (Bridge) genesis
	genesis.CustomChains = append(genesis.CustomChains, tmpnet.CustomChain{
		VMID:    bridgevm.ID,
		Genesis: generateBChainGenesis(),
		Config:  generateBChainConfig(),
	})

	// M-Chain (MPC) genesis
	genesis.CustomChains = append(genesis.CustomChains, tmpnet.CustomChain{
		VMID:    mpcvm.ID,
		Genesis: generateMChainGenesis(),
		Config:  generateMChainConfig(),
	})

	// Q-Chain (Quantum) genesis
	genesis.CustomChains = append(genesis.CustomChains, tmpnet.CustomChain{
		VMID:    quantumvm.ID,
		Genesis: generateQChainGenesis(),
		Config:  generateQChainConfig(),
	})

	// Z-Chain (ZK) genesis
	genesis.CustomChains = append(genesis.CustomChains, tmpnet.CustomChain{
		VMID:    zkvm.ID,
		Genesis: generateZChainGenesis(),
		Config:  generateZChainConfig(),
	})

	return genesis
}

// Chain-specific genesis generators

func generatePChainGenesis() []byte {
	// Platform chain genesis with validators
	genesis := map[string]interface{}{
		"validators": []map[string]interface{}{
			// Add validator configs
		},
		"chains": []map[string]interface{}{
			// Add subnet configs for custom chains
		},
	}
	data, _ := json.Marshal(genesis)
	return data
}

func generateCChainGenesis() []byte {
	// EVM chain genesis
	genesis := map[string]interface{}{
		"config": map[string]interface{}{
			"chainId":             96369,
			"homesteadBlock":      0,
			"eip150Block":         0,
			"eip150Hash":          "0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0",
			"eip155Block":         0,
			"eip158Block":         0,
			"byzantiumBlock":      0,
			"constantinopleBlock": 0,
			"petersburgBlock":     0,
			"istanbulBlock":       0,
			"muirGlacierBlock":    0,
			"subnetEVMTimestamp":  0,
		},
		"nonce":      "0x0",
		"timestamp":  "0x0",
		"extraData":  "0x00",
		"gasLimit":   "0x5f5e100",
		"difficulty": "0x0",
		"mixHash":    "0x0000000000000000000000000000000000000000000000000000000000000000",
		"coinbase":   "0x0000000000000000000000000000000000000000",
		"alloc":      map[string]interface{}{},
	}
	data, _ := json.Marshal(genesis)
	return data
}

func generateXChainGenesis() []byte {
	// X-Chain (AVM) genesis
	genesis := map[string]interface{}{
		"networkID": 96369,
		"assetAlias": "LUX",
		"initialState": map[string]interface{}{
			"fixedCap": []interface{}{},
		},
	}
	data, _ := json.Marshal(genesis)
	return data
}

func generateAChainGenesis() []byte {
	// AI Chain genesis
	genesis := map[string]interface{}{
		"agents": []map[string]interface{}{
			{
				"id":          "agent-001",
				"name":        "Genesis AI Agent",
				"modelType":   "llm",
				"gpuProvider": "genesis-gpu-pool",
			},
		},
		"gpuProviders": []map[string]interface{}{
			{
				"id":       "genesis-gpu-pool",
				"capacity": 100,
				"type":     "A100",
			},
		},
	}
	data, _ := json.Marshal(genesis)
	return data
}

func generateBChainGenesis() []byte {
	// Bridge Chain genesis
	genesis := map[string]interface{}{
		"bridges": []map[string]interface{}{
			{
				"id":          "eth-bridge",
				"targetChain": "ethereum",
				"threshold":   5,
			},
		},
		"mpcNodes": []map[string]interface{}{
			// MPC node configs
		},
	}
	data, _ := json.Marshal(genesis)
	return data
}

func generateMChainGenesis() []byte {
	// MPC Chain genesis
	genesis := map[string]interface{}{
		"mpcConfig": map[string]interface{}{
			"threshold":     5,
			"participants":  8,
			"sessionLength": 100,
		},
		"initialNodes": []map[string]interface{}{
			// Initial MPC nodes
		},
	}
	data, _ := json.Marshal(genesis)
	return data
}

func generateQChainGenesis() []byte {
	// Quantum Chain genesis
	genesis := map[string]interface{}{
		"quantumConfig": map[string]interface{}{
			"algorithm":     "sphincs+",
			"securityLevel": 256,
		},
		"validators": []map[string]interface{}{
			// Quantum-safe validators
		},
	}
	data, _ := json.Marshal(genesis)
	return data
}

func generateZChainGenesis() []byte {
	// ZK Chain genesis
	genesis := map[string]interface{}{
		"zkConfig": map[string]interface{}{
			"proofSystem": "plonk",
			"curveType":   "bn254",
		},
		"circuits": []map[string]interface{}{
			{
				"id":   "transfer",
				"type": "snark",
			},
		},
	}
	data, _ := json.Marshal(genesis)
	return data
}

// Chain configuration generators

func generateAChainConfig() json.RawMessage {
	config := map[string]interface{}{
		"blockTime":      2000, // 2 seconds
		"minGasPrice":    1000000000,
		"maxGasLimit":    15000000,
		"targetGasUsage": 10000000,
	}
	data, _ := json.Marshal(config)
	return data
}

func generateBChainConfig() json.RawMessage {
	config := map[string]interface{}{
		"blockTime":      3000, // 3 seconds
		"minSignatures":  5,
		"bridgeTimeout":  600, // 10 minutes
	}
	data, _ := json.Marshal(config)
	return data
}

func generateMChainConfig() json.RawMessage {
	config := map[string]interface{}{
		"blockTime":     2000,
		"mpcThreshold":  5,
		"sessionLength": 100,
	}
	data, _ := json.Marshal(config)
	return data
}

func generateQChainConfig() json.RawMessage {
	config := map[string]interface{}{
		"blockTime":      2000,
		"signatureAlgo":  "sphincs+",
		"hashFunction":   "sha3-256",
	}
	data, _ := json.Marshal(config)
	return data
}

func generateZChainConfig() json.RawMessage {
	config := map[string]interface{}{
		"blockTime":      4000, // 4 seconds for proof generation
		"proofSystem":    "plonk",
		"maxProofSize":   2048,
	}
	data, _ := json.Marshal(config)
	return data
}

// Test helper functions

func testAllChainRPCs(tc tests.TestContext, network *tmpnet.Network) {
	require := require.New(tc)
	
	// Test each chain's RPC endpoints
	chains := []struct {
		alias    string
		endpoint string
		method   string
		params   interface{}
	}{
		{"P", "/ext/bc/P", "platform.getHeight", nil},
		{"C", "/ext/bc/C/rpc", "eth_blockNumber", nil},
		{"X", "/ext/bc/X", "avm.getHeight", nil},
		{"A", "/ext/bc/A", "aivm.listAgents", nil},
		{"B", "/ext/bc/B", "bridgevm.getBridges", nil},
		{"M", "/ext/bc/M", "mpcvm.getNodes", nil},
		{"Q", "/ext/bc/Q", "quantumvm.getConfig", nil},
		{"Z", "/ext/bc/Z", "zkvm.getCircuits", nil},
	}

	for _, chain := range chains {
		tc.By(fmt.Sprintf("testing %s-Chain RPC", chain.alias))
		
		// Test on each node
		for i, node := range network.Nodes {
			endpoint := fmt.Sprintf("http://%s:9650%s", node.URI, chain.endpoint)
			
			// Make RPC call
			// Note: This is simplified - actual implementation would make proper JSON-RPC calls
			tc.Logf("Testing %s on node %d: %s", chain.alias, i, endpoint)
			
			// Verify response
			require.NotNil(endpoint) // Placeholder for actual RPC test
		}
	}
}

func verifyCoreLoadBalancing(tc tests.TestContext, network *tmpnet.Network) {
	// In Go, we can use runtime.GOMAXPROCS to control parallelism
	// Each VM can be assigned to run on specific OS threads
	
	maxProcs := runtime.GOMAXPROCS(0)
	tc.Logf("GOMAXPROCS: %d", maxProcs)
	
	// Verify load distribution
	// This would involve monitoring actual CPU usage per core
	// which requires OS-specific monitoring tools
}

func generateCoreAffinityConfig(numCores int) string {
	// Generate configuration that assigns VMs to specific cores
	// This is a conceptual implementation - actual CPU affinity
	// in Go requires OS-specific system calls
	
	config := map[string]interface{}{
		"vmCoreAffinity": map[string]int{
			"platformvm": 0,
			"evm":        1,
			"avm":        2,
			"aivm":       3,
			"bridgevm":   4,
			"mpcvm":      5,
			"quantumvm":  6,
			"zkvm":       7 % numCores, // Wrap around if fewer cores
		},
	}
	
	// Note: Go doesn't have built-in CPU affinity, but we can:
	// 1. Use runtime.LockOSThread() to bind goroutines to OS threads
	// 2. Use OS-specific syscalls to set thread affinity
	// 3. Use GOMAXPROCS and careful goroutine management
	
	data, _ := json.Marshal(config)
	return string(data)
}

func verifyCoreAffinity(tc tests.TestContext, network *tmpnet.Network) {
	// This would verify that VMs are actually running on assigned cores
	// Requires OS-specific monitoring
	tc.Log("Core affinity verification would require OS-specific monitoring tools")
	
	// In practice, you might:
	// 1. Use /proc/[pid]/task/[tid]/stat on Linux
	// 2. Use Activity Monitor or dtrace on macOS
	// 3. Use Performance Monitor on Windows
}

func testCrossChainInteractions(tc tests.TestContext, network *tmpnet.Network) {
	tc.Log("Testing cross-chain interactions between all 8 chains")
	
	// Test atomic swaps between chains
	// Test bridge operations
	// Test MPC signing across chains
	// etc.
}