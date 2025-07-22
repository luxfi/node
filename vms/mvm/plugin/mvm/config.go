// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mvm

import (
	"encoding/json"
	"time"

	"github.com/luxfi/node/network/p2p"
	"github.com/luxfi/node/vms/components/warp"
)

// Config contains all MVM configuration parameters
type Config struct {
	// Core Settings
	Enabled             bool          `json:"enabled"`
	MaxBlockSize        uint64        `json:"maxBlockSize"`
	MaxBlockGas         uint64        `json:"maxBlockGas"`
	BlockBuildInterval  time.Duration `json:"blockBuildInterval"`
	
	// Mempool Settings
	MempoolSize         int           `json:"mempoolSize"`
	IntentPoolSize      int           `json:"intentPoolSize"`
	
	// MPC Settings
	MPCEnabled          bool          `json:"mpcEnabled"`
	MPCConfig           MPCConfig     `json:"mpcConfig"`
	
	// ZK Settings
	ZKEnabled           bool          `json:"zkEnabled"`
	ZKConfig            ZKConfig      `json:"zkConfig"`
	
	// Teleport Protocol Settings
	TeleportEnabled     bool          `json:"teleportEnabled"`
	ExecutorConfig      ExecutorConfig `json:"executorConfig"`
	
	// X-Chain Settlement Settings
	XChainSettlement    bool          `json:"xChainSettlement"`
	XChainEndpoint      string        `json:"xChainEndpoint"`
	XChainAuth          string        `json:"xChainAuth"`
	
	// Warp Settings
	WarpConfig          warp.Config   `json:"warpConfig"`
	
	// P2P Settings
	P2PConfig           p2p.Config    `json:"p2pConfig"`
	
	// API Settings
	APIEnabled          []string      `json:"apiEnabled"`
	APIMaxDuration      time.Duration `json:"apiMaxDuration"`
	
	// State Sync
	StateSyncEnabled    bool          `json:"stateSyncEnabled"`
	StateSyncServerAddr string        `json:"stateSyncServerAddr"`
}

// MPCConfig contains MPC-specific configuration
type MPCConfig struct {
	// CGG21 Parameters
	Threshold           int           `json:"threshold"`
	PartyCount          int           `json:"partyCount"`
	KeyGenTimeout       time.Duration `json:"keyGenTimeout"`
	SignTimeout         time.Duration `json:"signTimeout"`
	ReshareTimeout      time.Duration `json:"reshareTimeout"`
	
	// Session Management
	MaxConcurrentSessions int         `json:"maxConcurrentSessions"`
	SessionTimeout       time.Duration `json:"sessionTimeout"`
	
	// Security Parameters
	EnablePresharing    bool          `json:"enablePresharing"`
	EnableIdentifiableAbort bool      `json:"enableIdentifiableAbort"`
}

// ZKConfig contains ZK proof configuration
type ZKConfig struct {
	// Proof System
	ProofSystem         string        `json:"proofSystem"` // "groth16", "plonk", "stark"
	CircuitPath         string        `json:"circuitPath"`
	ProvingKeyPath      string        `json:"provingKeyPath"`
	VerifyingKeyPath    string        `json:"verifyingKeyPath"`
	
	// Performance
	MaxProofGenTime     time.Duration `json:"maxProofGenTime"`
	ProofCacheSize      int           `json:"proofCacheSize"`
}

// ExecutorConfig contains Teleport executor configuration
type ExecutorConfig struct {
	// Execution Parameters
	MaxIntentAge        time.Duration `json:"maxIntentAge"`
	MinExecutorStake    uint64        `json:"minExecutorStake"`
	ExecutionTimeout    time.Duration `json:"executionTimeout"`
	
	// X-Chain Settlement
	SettlementBatchSize int           `json:"settlementBatchSize"`
	SettlementInterval  time.Duration `json:"settlementInterval"`
	
	// Fee Configuration
	BaseFeePercent      float64       `json:"baseFeePercent"`
	MaxSlippage         float64       `json:"maxSlippage"`
	
	// Liquidity Management
	MinLiquidityBuffer  uint64        `json:"minLiquidityBuffer"`
	AutoRebalance       bool          `json:"autoRebalance"`
}

// DefaultConfig returns default MVM configuration
func DefaultConfig() Config {
	return Config{
		Enabled:            true,
		MaxBlockSize:       2 * 1024 * 1024, // 2MB
		MaxBlockGas:        10_000_000,
		BlockBuildInterval: 2 * time.Second,
		
		MempoolSize:        10000,
		IntentPoolSize:     5000,
		
		MPCEnabled:         true,
		MPCConfig: MPCConfig{
			Threshold:              67, // 2/3 + 1 for 100 validators
			PartyCount:            100,
			KeyGenTimeout:         5 * time.Minute,
			SignTimeout:           30 * time.Second,
			ReshareTimeout:        10 * time.Minute,
			MaxConcurrentSessions: 100,
			SessionTimeout:        1 * time.Hour,
			EnablePresharing:      true,
			EnableIdentifiableAbort: true,
		},
		
		ZKEnabled:          true,
		ZKConfig: ZKConfig{
			ProofSystem:     "groth16",
			MaxProofGenTime: 30 * time.Second,
			ProofCacheSize:  1000,
		},
		
		TeleportEnabled:    true,
		ExecutorConfig: ExecutorConfig{
			MaxIntentAge:        5 * time.Minute,
			MinExecutorStake:    1000000, // 1M LUX
			ExecutionTimeout:    2 * time.Minute,
			SettlementBatchSize: 100,
			SettlementInterval:  30 * time.Second,
			BaseFeePercent:      0.3,
			MaxSlippage:         1.0,
			MinLiquidityBuffer:  1000000,
			AutoRebalance:       true,
		},
		
		XChainSettlement:   true,
		XChainEndpoint:     "http://localhost:9650/ext/bc/X",
		
		APIEnabled: []string{
			"mvm",
			"teleport",
			"mpc",
			"validators",
		},
		APIMaxDuration:     10 * time.Minute,
		
		StateSyncEnabled:   true,
	}
}

// Parse parses configuration from bytes
func (c *Config) Parse(configBytes []byte) error {
	// Start with defaults
	*c = DefaultConfig()
	
	// Override with provided config
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, c); err != nil {
			return err
		}
	}
	
	return c.Validate()
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.MPCEnabled && c.MPCConfig.Threshold > c.MPCConfig.PartyCount {
		return fmt.Errorf("MPC threshold cannot exceed party count")
	}
	
	if c.TeleportEnabled && !c.MPCEnabled {
		return fmt.Errorf("Teleport Protocol requires MPC to be enabled")
	}
	
	if c.XChainSettlement && c.XChainEndpoint == "" {
		return fmt.Errorf("X-Chain endpoint required when X-Chain settlement is enabled")
	}
	
	return nil
}