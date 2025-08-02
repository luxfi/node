// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utils/constants"
	"github.com/luxfi/node/v2/utils/formatting/address"
	"github.com/luxfi/node/v2/utils/units"
	"github.com/luxfi/node/v2/vms/aivm"
	"github.com/luxfi/node/v2/vms/bridgevm"
	"github.com/luxfi/node/v2/vms/mpcvm"
	"github.com/luxfi/node/v2/vms/platformvm/signer"
	"github.com/luxfi/node/v2/vms/quantumvm"
	"github.com/luxfi/node/v2/vms/zkvm"
)

// Genesis8ChainsParams contains parameters for generating 8-chain genesis
type Genesis8ChainsParams struct {
	NetworkID       uint32
	NumValidators   int
	InitialStake    uint64
	StakingDuration time.Duration
	
	// Chain-specific parameters
	EVMChainID      *big.Int
	AIAgentCount    int
	BridgeThreshold int
	MPCParticipants int
	ZKCircuitCount  int
}

// Default8ChainsParams returns default parameters for 8-chain genesis
func Default8ChainsParams() Genesis8ChainsParams {
	return Genesis8ChainsParams{
		NetworkID:       constants.LocalID,
		NumValidators:   8,
		InitialStake:    2000 * units.Lux,
		StakingDuration: 365 * 24 * time.Hour,
		EVMChainID:      big.NewInt(96369),
		AIAgentCount:    10,
		BridgeThreshold: 5,
		MPCParticipants: 8,
		ZKCircuitCount:  5,
	}
}

// Generate8ChainsGenesis creates a complete genesis for all 8 chains
func Generate8ChainsGenesis(params Genesis8ChainsParams) (*Genesis, error) {
	// Create validators
	validators := make([]Validator, params.NumValidators)
	for i := 0; i < params.NumValidators; i++ {
		nodeID := ids.BuildTestNodeID([]byte(fmt.Sprintf("validator-%d", i)))
		
		validators[i] = Validator{
			NodeID: nodeID,
			Start:  uint64(time.Now().Unix()),
			End:    uint64(time.Now().Add(params.StakingDuration).Unix()),
			Weight: params.InitialStake,
		}
	}

	// Create base genesis
	genesis := &Genesis{
		NetworkID: params.NetworkID,
		Validators: validators,
		Chains: []Chain{
			// P-Chain
			{
				ChainID: constants.PlatformChainID,
				VMID:    constants.PlatformVMID,
				Genesis: generatePlatformGenesis(validators, params),
			},
			// X-Chain
			{
				ChainID: ids.GenerateTestID(),
				VMID:    constants.XVMID,
				Genesis: generateXChainGenesis(params),
			},
			// C-Chain (EVM)
			{
				ChainID: ids.GenerateTestID(),
				VMID:    constants.EVMID,
				Genesis: generateEVMGenesis(params),
			},
			// A-Chain (AI)
			{
				ChainID: ids.GenerateTestID(),
				VMID:    aivm.ID,
				Genesis: generateAIChainGenesis(params),
			},
			// B-Chain (Bridge)
			{
				ChainID: ids.GenerateTestID(),
				VMID:    bridgevm.ID,
				Genesis: generateBridgeChainGenesis(params),
			},
			// M-Chain (MPC)
			{
				ChainID: ids.GenerateTestID(),
				VMID:    mpcvm.ID,
				Genesis: generateMPCChainGenesis(params),
			},
			// Q-Chain (Quantum)
			{
				ChainID: ids.GenerateTestID(),
				VMID:    quantumvm.ID,
				Genesis: generateQuantumChainGenesis(params),
			},
			// Z-Chain (ZK)
			{
				ChainID: ids.GenerateTestID(),
				VMID:    zkvm.ID,
				Genesis: generateZKChainGenesis(params),
			},
		},
		InitialSupply: params.InitialStake * uint64(params.NumValidators) * 10, // 10x stake for circulation
	}

	return genesis, nil
}

// Chain-specific genesis generators

func generatePlatformGenesis(validators []Validator, params Genesis8ChainsParams) []byte {
	genesis := map[string]interface{}{
		"validators": validators,
		"chains":     []interface{}{}, // Subnets will be added here
		"initialStakeDuration": int64(params.StakingDuration.Seconds()),
		"initialStakeDurationOffset": 0,
		"initialStakedFunds": []interface{}{},
		"message": "Platform Chain Genesis",
	}
	
	data, _ := json.MarshalIndent(genesis, "", "  ")
	return data
}

func generateXChainGenesis(params Genesis8ChainsParams) []byte {
	genesis := map[string]interface{}{
		"networkID": params.NetworkID,
		"blockchain": map[string]interface{}{
			"vmID": constants.XVMID.String(),
			"name": "X-Chain",
			"memo": "Exchange Chain for asset transfers",
		},
		"representativeToken": map[string]interface{}{
			"symbol": "LUX",
			"name":   "Lux",
			"denomination": 9,
			"initialSupply": params.InitialStake * 10,
		},
		"initialState": map[string]interface{}{
			"fixedCap": []interface{}{
				// Initial UTXO allocations
			},
		},
	}
	
	data, _ := json.MarshalIndent(genesis, "", "  ")
	return data
}

func generateEVMGenesis(params Genesis8ChainsParams) []byte {
	genesis := map[string]interface{}{
		"config": map[string]interface{}{
			"chainId":             params.EVMChainID,
			"homesteadBlock":      0,
			"eip150Block":         0,
			"eip150Hash":          "0x0000000000000000000000000000000000000000000000000000000000000000",
			"eip155Block":         0,
			"eip158Block":         0,
			"byzantiumBlock":      0,
			"constantinopleBlock": 0,
			"petersburgBlock":     0,
			"istanbulBlock":       0,
			"muirGlacierBlock":    0,
			"subnetEVMTimestamp":  0,
			"feeConfig": map[string]interface{}{
				"gasLimit":        15000000,
				"targetBlockRate": 2,
				"minBaseFee":      25000000000,
				"targetGas":       15000000,
				"baseFeeChangeDenominator": 36,
				"minBlockGasCost": 0,
				"maxBlockGasCost": 1000000,
				"blockGasCostStep": 200000,
			},
		},
		"nonce":      "0x0",
		"timestamp":  fmt.Sprintf("0x%x", time.Now().Unix()),
		"extraData":  "0x00",
		"gasLimit":   "0xe4e1c0",
		"difficulty": "0x0",
		"mixHash":    "0x0000000000000000000000000000000000000000000000000000000000000000",
		"coinbase":   "0x0000000000000000000000000000000000000000",
		"alloc": map[string]interface{}{
			// Pre-funded accounts
			"0x1000000000000000000000000000000000000001": map[string]interface{}{
				"balance": "0x52b7d2dcc80cd2e4000000", // 100M tokens
			},
		},
		"number":     "0x0",
		"gasUsed":    "0x0",
		"parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
	}
	
	data, _ := json.MarshalIndent(genesis, "", "  ")
	return data
}

func generateAIChainGenesis(params Genesis8ChainsParams) []byte {
	agents := make([]map[string]interface{}, params.AIAgentCount)
	for i := 0; i < params.AIAgentCount; i++ {
		agents[i] = map[string]interface{}{
			"id":          fmt.Sprintf("agent-%03d", i),
			"name":        fmt.Sprintf("AI Agent %d", i),
			"modelType":   "llm-7b",
			"gpuProvider": fmt.Sprintf("gpu-pool-%d", i%3),
			"reputation":  100,
			"stake":       1000 * units.Lux,
		}
	}

	genesis := map[string]interface{}{
		"config": map[string]interface{}{
			"blockTime":        2000, // 2 seconds
			"minGasPrice":      1000000000,
			"maxGasLimit":      15000000,
			"targetGasUsage":   10000000,
			"minAgentStake":    1000 * units.Lux,
			"taskTimeout":      300, // 5 minutes
			"reputationDecay":  0.99,
		},
		"agents": agents,
		"gpuProviders": []map[string]interface{}{
			{
				"id":       "gpu-pool-0",
				"capacity": 1000,
				"type":     "A100",
				"location": "us-east-1",
			},
			{
				"id":       "gpu-pool-1",
				"capacity": 500,
				"type":     "H100",
				"location": "us-west-2",
			},
			{
				"id":       "gpu-pool-2",
				"capacity": 750,
				"type":     "RTX4090",
				"location": "eu-central-1",
			},
		},
		"modelRegistry": []map[string]interface{}{
			{
				"id":         "llm-7b",
				"name":       "LLM 7B Base",
				"parameters": 7000000000,
				"gpuMemory":  16,
			},
		},
	}
	
	data, _ := json.MarshalIndent(genesis, "", "  ")
	return data
}

func generateBridgeChainGenesis(params Genesis8ChainsParams) []byte {
	genesis := map[string]interface{}{
		"config": map[string]interface{}{
			"blockTime":       3000, // 3 seconds
			"minSignatures":   params.BridgeThreshold,
			"bridgeTimeout":   600,  // 10 minutes
			"maxBridgeAmount": 1000000 * units.Lux,
			"bridgeFee":       0.001, // 0.1%
		},
		"bridges": []map[string]interface{}{
			{
				"id":          "eth-mainnet",
				"targetChain": "ethereum",
				"chainId":     1,
				"threshold":   params.BridgeThreshold,
				"address":     "0x0000000000000000000000000000000000000000",
				"status":      "active",
			},
			{
				"id":          "bsc-mainnet",
				"targetChain": "bsc",
				"chainId":     56,
				"threshold":   params.BridgeThreshold,
				"address":     "0x0000000000000000000000000000000000000000",
				"status":      "active",
			},
		},
		"mpcNodes": generateMPCNodes(params.MPCParticipants),
		"treasury": map[string]interface{}{
			"address": "0x0000000000000000000000000000000000000001",
			"balance": 10000 * units.Lux,
		},
	}
	
	data, _ := json.MarshalIndent(genesis, "", "  ")
	return data
}

func generateMPCChainGenesis(params Genesis8ChainsParams) []byte {
	genesis := map[string]interface{}{
		"config": map[string]interface{}{
			"blockTime":          2000,
			"mpcThreshold":       params.BridgeThreshold,
			"sessionLength":      100,
			"keyGenTimeout":      60,
			"signTimeout":        30,
			"minParticipants":    params.BridgeThreshold,
			"maxParticipants":    params.MPCParticipants,
			"slashingPenalty":    100 * units.Lux,
			"sessionReward":      10 * units.Lux,
		},
		"initialNodes": generateMPCNodes(params.MPCParticipants),
		"protocols": []map[string]interface{}{
			{
				"id":        "gg20",
				"name":      "Gennaro-Goldfeder 2020",
				"threshold": params.BridgeThreshold,
				"supported": true,
			},
			{
				"id":        "frost",
				"name":      "FROST",
				"threshold": params.BridgeThreshold,
				"supported": true,
			},
		},
		"sessions": []interface{}{},
	}
	
	data, _ := json.MarshalIndent(genesis, "", "  ")
	return data
}

func generateQuantumChainGenesis(params Genesis8ChainsParams) []byte {
	genesis := map[string]interface{}{
		"config": map[string]interface{}{
			"blockTime":      2000,
			"signatureAlgo":  "sphincs+",
			"hashFunction":   "sha3-256",
			"securityLevel":  256,
			"publicKeySize":  64,
			"signatureSize":  49856, // SPHINCS+ signature size
			"migrationStart": time.Now().Add(365 * 24 * time.Hour).Unix(),
		},
		"validators": make([]map[string]interface{}, 0),
		"quantumAlgorithms": []map[string]interface{}{
			{
				"id":            "sphincs+",
				"name":          "SPHINCS+",
				"type":          "signature",
				"securityLevel": 256,
				"status":        "active",
			},
			{
				"id":            "dilithium",
				"name":          "CRYSTALS-Dilithium",
				"type":          "signature",
				"securityLevel": 256,
				"status":        "experimental",
			},
			{
				"id":            "kyber",
				"name":          "CRYSTALS-Kyber",
				"type":          "kem",
				"securityLevel": 256,
				"status":        "experimental",
			},
		},
		"migrationPlan": map[string]interface{}{
			"phase1Start": time.Now().Add(180 * 24 * time.Hour).Unix(),
			"phase2Start": time.Now().Add(365 * 24 * time.Hour).Unix(),
			"mandatory":   time.Now().Add(730 * 24 * time.Hour).Unix(),
		},
	}
	
	data, _ := json.MarshalIndent(genesis, "", "  ")
	return data
}

func generateZKChainGenesis(params Genesis8ChainsParams) []byte {
	circuits := make([]map[string]interface{}, params.ZKCircuitCount)
	circuitTypes := []string{"transfer", "mint", "burn", "swap", "stake"}
	
	for i := 0; i < params.ZKCircuitCount; i++ {
		circuits[i] = map[string]interface{}{
			"id":             fmt.Sprintf("circuit-%03d", i),
			"name":           fmt.Sprintf("ZK %s Circuit", circuitTypes[i%len(circuitTypes)]),
			"type":           circuitTypes[i%len(circuitTypes)],
			"proofSystem":    "plonk",
			"constraintSize": 100000 + i*10000,
			"setupComplete":  true,
			"srsHash":        fmt.Sprintf("0x%064x", i),
		}
	}

	genesis := map[string]interface{}{
		"config": map[string]interface{}{
			"blockTime":         4000, // 4 seconds for proof generation
			"proofSystem":       "plonk",
			"curveType":         "bn254",
			"maxProofSize":      2048,
			"maxConstraints":    1000000,
			"proofGenTimeout":   30,
			"proofVerifyTime":   10,
			"recursionDepth":    3,
			"batchSize":         100,
		},
		"circuits": circuits,
		"trustedSetup": map[string]interface{}{
			"ceremonyDate": time.Now().Unix(),
			"participants": 100,
			"srsHash":      "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			"verified":     true,
		},
		"verifierRegistry": []map[string]interface{}{
			{
				"id":       "plonk-verifier",
				"version":  "1.0.0",
				"gasUsage": 300000,
			},
		},
		"privacyPools": []map[string]interface{}{
			{
				"id":           "default-pool",
				"minDeposit":   1 * units.Lux,
				"maxDeposit":   1000 * units.Lux,
				"participants": 0,
			},
		},
	}
	
	data, _ := json.MarshalIndent(genesis, "", "  ")
	return data
}

// Helper functions

func generateMPCNodes(count int) []map[string]interface{} {
	nodes := make([]map[string]interface{}, count)
	for i := 0; i < count; i++ {
		nodeID := ids.BuildTestNodeID([]byte(fmt.Sprintf("mpc-node-%d", i)))
		nodes[i] = map[string]interface{}{
			"id":         nodeID.String(),
			"index":      i,
			"publicKey":  fmt.Sprintf("0x%064x", i),
			"stake":      1000 * units.Lux,
			"reputation": 100,
			"endpoint":   fmt.Sprintf("mpc-node-%d:9651", i),
		}
	}
	return nodes
}

// ProcessorAffinityConfig generates CPU affinity configuration for 8 chains
func ProcessorAffinityConfig() map[string]interface{} {
	numCPU := 8 // Assume 8 cores for optimal distribution
	
	return map[string]interface{}{
		"processorAffinity": map[string]interface{}{
			"enabled": true,
			"vmAssignments": map[string]int{
				"platformvm": 0,
				"evm":        1,
				"avm":        2,
				"aivm":       3,
				"bridgevm":   4,
				"mpcvm":      5,
				"quantumvm":  6,
				"zkvm":       7,
			},
			"loadBalancing": map[string]interface{}{
				"algorithm": "round-robin",
				"rebalanceInterval": 60, // seconds
				"cpuThreshold": 80, // percentage
			},
		},
		"gomaxprocs": numCPU,
		"threadPool": map[string]interface{}{
			"coreThreads": numCPU,
			"maxThreads":  numCPU * 2,
			"queueSize":   1000,
		},
	}
}