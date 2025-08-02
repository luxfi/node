// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/units"
)

// mustToID converts bytes to ID, panics on error
func mustToID(data []byte) ids.ID {
	id, err := ids.ToID(data)
	if err != nil {
		panic(err)
	}
	return id
}

// EightChainsConfig defines the configuration for all 8 chains
type EightChainsConfig struct {
	// Core chains
	AChainConfig ChainConfig `json:"aChain"` // AI VM
	BChainConfig ChainConfig `json:"bChain"` // Bridge VM
	CChainConfig ChainConfig `json:"cChain"` // EVM
	MChainConfig ChainConfig `json:"mChain"` // MPC VM
	PChainConfig ChainConfig `json:"pChain"` // Platform VM
	QChainConfig ChainConfig `json:"qChain"` // Quantum VM
	XChainConfig ChainConfig `json:"xChain"` // Exchange VM
	ZChainConfig ChainConfig `json:"zChain"` // ZK VM

	// Network-wide settings
	NetworkID      uint32 `json:"networkID"`
	ProcessorCount int    `json:"processorCount"`
}

// ChainConfig represents per-chain configuration
type ChainConfig struct {
	// Chain identification
	ChainID   ids.ID `json:"chainID"`
	VMID      ids.ID `json:"vmID"`
	ChainName string `json:"chainName"`

	// Consensus parameters
	ConsensusParams ConsensusParams `json:"consensusParams"`

	// Resource allocation
	ProcessorAffinity []int  `json:"processorAffinity"` // CPU cores
	MemoryLimit       uint64 `json:"memoryLimit"`       // In bytes
	DiskQuota         uint64 `json:"diskQuota"`         // In bytes

	// Network settings
	MaxMessageSize      int           `json:"maxMessageSize"`
	MaxPendingMessages  int           `json:"maxPendingMessages"`
	GossipFrequency     time.Duration `json:"gossipFrequency"`
	GossipBatchSize     int           `json:"gossipBatchSize"`

	// Performance tuning
	BlockCacheSize      int `json:"blockCacheSize"`
	StateCacheSize      int `json:"stateCacheSize"`
	TransactionPoolSize int `json:"transactionPoolSize"`

	// Chain-specific settings
	CustomConfig interface{} `json:"customConfig,omitempty"`
}

// ConsensusParams defines consensus parameters for a chain
type ConsensusParams struct {
	K                     int           `json:"k"`
	AlphaPreference       int           `json:"alphaPreference"`
	AlphaConfidence       int           `json:"alphaConfidence"`
	Beta                  int           `json:"beta"`
	MaxItemProcessingTime time.Duration `json:"maxItemProcessingTime"`
	
	// Additional tuning
	ConcurrentRepolls       int           `json:"concurrentRepolls"`
	OptimalProcessing       int           `json:"optimalProcessing"`
	MaxOutstandingItems     int           `json:"maxOutstandingItems"`
	MaxItemProcessingRetry  int           `json:"maxItemProcessingRetry"`
	ParentCheckInterval     time.Duration `json:"parentCheckInterval"`
}

// Default8ChainsConfig returns the default configuration for 8 chains
func Default8ChainsConfig() *EightChainsConfig {
	return &EightChainsConfig{
		NetworkID:      constants.MainnetID,
		ProcessorCount: 8,

		// A-Chain: AI VM (Fast consensus for AI operations)
		AChainConfig: ChainConfig{
			ChainID:           mustToID([]byte("achain")),
			VMID:              constants.AIVMID,
			ChainName:         "A-Chain",
			ProcessorAffinity: []int{0}, // CPU 0
			MemoryLimit:       8 * units.GiB,
			DiskQuota:         100 * units.GiB,
			ConsensusParams: ConsensusParams{
				K:                     5,
				AlphaPreference:       3,
				AlphaConfidence:       4,
				Beta:                  3,
				MaxItemProcessingTime: 3 * time.Second,
				ConcurrentRepolls:     4,
				OptimalProcessing:     10,
				MaxOutstandingItems:   256,
			},
			MaxMessageSize:      4 * units.MiB,
			MaxPendingMessages:  1024,
			GossipFrequency:     250 * time.Millisecond,
			GossipBatchSize:     32,
			BlockCacheSize:      4096,
			StateCacheSize:      8192,
			TransactionPoolSize: 4096,
		},

		// B-Chain: Bridge VM (Balanced for cross-chain)
		BChainConfig: ChainConfig{
			ChainID:           mustToID([]byte("bchain")),
			VMID:              constants.BridgeVMID,
			ChainName:         "B-Chain",
			ProcessorAffinity: []int{1}, // CPU 1
			MemoryLimit:       4 * units.GiB,
			DiskQuota:         50 * units.GiB,
			ConsensusParams: ConsensusParams{
				K:                     11,
				AlphaPreference:       7,
				AlphaConfidence:       9,
				Beta:                  6,
				MaxItemProcessingTime: 6300 * time.Millisecond,
				ConcurrentRepolls:     4,
				OptimalProcessing:     5,
				MaxOutstandingItems:   128,
			},
			MaxMessageSize:      2 * units.MiB,
			MaxPendingMessages:  512,
			GossipFrequency:     500 * time.Millisecond,
			GossipBatchSize:     16,
			BlockCacheSize:      2048,
			StateCacheSize:      4096,
			TransactionPoolSize: 2048,
		},

		// C-Chain: EVM (Balanced for smart contracts)
		CChainConfig: ChainConfig{
			ChainID:           mustToID([]byte("cchain")),
			VMID:              constants.EVMID,
			ChainName:         "C-Chain",
			ProcessorAffinity: []int{2}, // CPU 2
			MemoryLimit:       8 * units.GiB,
			DiskQuota:         200 * units.GiB,
			ConsensusParams: ConsensusParams{
				K:                     11,
				AlphaPreference:       7,
				AlphaConfidence:       9,
				Beta:                  6,
				MaxItemProcessingTime: 6300 * time.Millisecond,
				ConcurrentRepolls:     4,
				OptimalProcessing:     10,
				MaxOutstandingItems:   256,
			},
			MaxMessageSize:      8 * units.MiB,
			MaxPendingMessages:  1024,
			GossipFrequency:     500 * time.Millisecond,
			GossipBatchSize:     32,
			BlockCacheSize:      8192,
			StateCacheSize:      16384,
			TransactionPoolSize: 8192,
		},

		// M-Chain: MPC VM (Secure consensus for multi-party computation)
		MChainConfig: ChainConfig{
			ChainID:           mustToID([]byte("mchain")),
			VMID:              constants.MPCVMID,
			ChainName:         "M-Chain",
			ProcessorAffinity: []int{3}, // CPU 3
			MemoryLimit:       4 * units.GiB,
			DiskQuota:         50 * units.GiB,
			ConsensusParams: ConsensusParams{
				K:                     21,
				AlphaPreference:       13,
				AlphaConfidence:       18,
				Beta:                  8,
				MaxItemProcessingTime: 9630 * time.Millisecond,
				ConcurrentRepolls:     2,
				OptimalProcessing:     3,
				MaxOutstandingItems:   64,
			},
			MaxMessageSize:      2 * units.MiB,
			MaxPendingMessages:  256,
			GossipFrequency:     1 * time.Second,
			GossipBatchSize:     8,
			BlockCacheSize:      1024,
			StateCacheSize:      2048,
			TransactionPoolSize: 512,
		},

		// P-Chain: Platform VM (Balanced for platform operations)
		PChainConfig: ChainConfig{
			ChainID:           constants.PlatformChainID,
			VMID:              constants.PlatformVMID,
			ChainName:         "P-Chain",
			ProcessorAffinity: []int{4}, // CPU 4
			MemoryLimit:       4 * units.GiB,
			DiskQuota:         100 * units.GiB,
			ConsensusParams: ConsensusParams{
				K:                     11,
				AlphaPreference:       7,
				AlphaConfidence:       9,
				Beta:                  6,
				MaxItemProcessingTime: 6300 * time.Millisecond,
				ConcurrentRepolls:     4,
				OptimalProcessing:     5,
				MaxOutstandingItems:   128,
			},
			MaxMessageSize:      2 * units.MiB,
			MaxPendingMessages:  512,
			GossipFrequency:     500 * time.Millisecond,
			GossipBatchSize:     16,
			BlockCacheSize:      2048,
			StateCacheSize:      4096,
			TransactionPoolSize: 1024,
		},

		// Q-Chain: Quantum VM (Balanced, powered by Quasar)
		QChainConfig: ChainConfig{
			ChainID:           mustToID([]byte("qchain")),
			VMID:              constants.QuantumVMID,
			ChainName:         "Q-Chain",
			ProcessorAffinity: []int{5}, // CPU 5
			MemoryLimit:       4 * units.GiB,
			DiskQuota:         100 * units.GiB,
			ConsensusParams: ConsensusParams{
				K:                     11,
				AlphaPreference:       7,
				AlphaConfidence:       9,
				Beta:                  6,
				MaxItemProcessingTime: 6300 * time.Millisecond,
				ConcurrentRepolls:     4,
				OptimalProcessing:     8,
				MaxOutstandingItems:   192,
			},
			MaxMessageSize:      4 * units.MiB,
			MaxPendingMessages:  768,
			GossipFrequency:     400 * time.Millisecond,
			GossipBatchSize:     24,
			BlockCacheSize:      3072,
			StateCacheSize:      6144,
			TransactionPoolSize: 3072,
		},

		// X-Chain: Exchange VM (Fast consensus for trading)
		XChainConfig: ChainConfig{
			ChainID:           mustToID([]byte("xchain")),
			VMID:              constants.XVMID,
			ChainName:         "X-Chain",
			ProcessorAffinity: []int{6}, // CPU 6
			MemoryLimit:       4 * units.GiB,
			DiskQuota:         100 * units.GiB,
			ConsensusParams: ConsensusParams{
				K:                     5,
				AlphaPreference:       3,
				AlphaConfidence:       4,
				Beta:                  3,
				MaxItemProcessingTime: 3 * time.Second,
				ConcurrentRepolls:     4,
				OptimalProcessing:     10,
				MaxOutstandingItems:   256,
			},
			MaxMessageSize:      2 * units.MiB,
			MaxPendingMessages:  1024,
			GossipFrequency:     250 * time.Millisecond,
			GossipBatchSize:     32,
			BlockCacheSize:      4096,
			StateCacheSize:      8192,
			TransactionPoolSize: 4096,
		},

		// Z-Chain: ZK VM (Secure consensus for zero-knowledge proofs)
		ZChainConfig: ChainConfig{
			ChainID:           mustToID([]byte("zchain")),
			VMID:              constants.ZKVMID,
			ChainName:         "Z-Chain",
			ProcessorAffinity: []int{7}, // CPU 7
			MemoryLimit:       4 * units.GiB,
			DiskQuota:         50 * units.GiB,
			ConsensusParams: ConsensusParams{
				K:                     21,
				AlphaPreference:       13,
				AlphaConfidence:       18,
				Beta:                  8,
				MaxItemProcessingTime: 9630 * time.Millisecond,
				ConcurrentRepolls:     2,
				OptimalProcessing:     3,
				MaxOutstandingItems:   64,
			},
			MaxMessageSize:      2 * units.MiB,
			MaxPendingMessages:  256,
			GossipFrequency:     1 * time.Second,
			GossipBatchSize:     8,
			BlockCacheSize:      1024,
			StateCacheSize:      2048,
			TransactionPoolSize: 512,
		},
	}
}

// OptimizeFor8Cores adjusts parameters for optimal 8-core performance
func (c *EightChainsConfig) OptimizeFor8Cores() {
	// Ensure each chain has dedicated CPU affinity
	c.AChainConfig.ProcessorAffinity = []int{0}
	c.BChainConfig.ProcessorAffinity = []int{1}
	c.CChainConfig.ProcessorAffinity = []int{2}
	c.MChainConfig.ProcessorAffinity = []int{3}
	c.PChainConfig.ProcessorAffinity = []int{4}
	c.QChainConfig.ProcessorAffinity = []int{5}
	c.XChainConfig.ProcessorAffinity = []int{6}
	c.ZChainConfig.ProcessorAffinity = []int{7}

	// Optimize cache sizes based on L3 cache sharing
	// Assuming 32MB L3 cache shared across 8 cores = 4MB per core
	// Leave room for OS and other processes
	maxCachePerChain := 3 * units.MiB

	chains := []*ChainConfig{
		&c.AChainConfig, &c.BChainConfig, &c.CChainConfig, &c.MChainConfig,
		&c.PChainConfig, &c.QChainConfig, &c.XChainConfig, &c.ZChainConfig,
	}

	for _, chain := range chains {
		// Calculate optimal cache sizes based on chain characteristics
		totalCacheSize := chain.BlockCacheSize + chain.StateCacheSize
		if totalCacheSize > int(maxCachePerChain) {
			// Scale down proportionally
			ratio := float64(maxCachePerChain) / float64(totalCacheSize)
			chain.BlockCacheSize = int(float64(chain.BlockCacheSize) * ratio)
			chain.StateCacheSize = int(float64(chain.StateCacheSize) * ratio)
		}
	}
}

// ChainGrouping represents how chains are grouped for optimization
type ChainGrouping struct {
	FastChains     []string // A, X - Low latency requirements
	SecureChains   []string // M, Z - High security requirements
	BalancedChains []string // B, C, P, Q - Balanced requirements
}

// GetChainGrouping returns the logical grouping of chains
func GetChainGrouping() ChainGrouping {
	return ChainGrouping{
		FastChains:     []string{"A-Chain", "X-Chain"},
		SecureChains:   []string{"M-Chain", "Z-Chain"},
		BalancedChains: []string{"B-Chain", "C-Chain", "P-Chain", "Q-Chain"},
	}
}

// WorkloadDistribution defines how work is distributed across chains
type WorkloadDistribution struct {
	// Chunk size for parallel processing
	ChunkSize int

	// Worker pool sizes per chain type
	FastChainWorkers     int
	SecureChainWorkers   int
	BalancedChainWorkers int

	// Message batching
	BatchSizes map[string]int
}

// GetOptimalWorkloadDistribution returns optimal workload distribution for 8 chains
func GetOptimalWorkloadDistribution() WorkloadDistribution {
	return WorkloadDistribution{
		ChunkSize: 8, // Process in chunks of 8 for optimal parallelism

		// Worker pools based on chain characteristics
		FastChainWorkers:     16, // More workers for high-throughput chains
		SecureChainWorkers:   4,  // Fewer workers for security-focused chains
		BalancedChainWorkers: 8,  // Moderate workers for balanced chains

		// Batch sizes optimized for each chain
		BatchSizes: map[string]int{
			"A-Chain": 32, // High throughput for AI operations
			"B-Chain": 16, // Moderate for bridge operations
			"C-Chain": 32, // High for EVM transactions
			"M-Chain": 8,  // Lower for MPC operations
			"P-Chain": 16, // Moderate for platform operations
			"Q-Chain": 24, // Higher for quantum operations
			"X-Chain": 32, // High for exchange operations
			"Z-Chain": 8,  // Lower for ZK proofs
		},
	}
}