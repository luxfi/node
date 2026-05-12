// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainadapter

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Registry manages chain adapters
type Registry struct {
	mu       sync.RWMutex
	adapters map[ChainID]ChainAdapter
	configs  map[ChainID]*ChainConfig
	metrics  map[ChainID]*ChainMetrics
}

// NewRegistry creates a new chain adapter registry
func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[ChainID]ChainAdapter),
		configs:  make(map[ChainID]*ChainConfig),
		metrics:  make(map[ChainID]*ChainMetrics),
	}
}

// Register registers a chain adapter
func (r *Registry) Register(adapter ChainAdapter, config *ChainConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	chainID := adapter.ChainID()
	if _, exists := r.adapters[chainID]; exists {
		return fmt.Errorf("adapter for chain %d already registered", chainID)
	}

	if err := adapter.Initialize(config); err != nil {
		return fmt.Errorf("failed to initialize adapter for chain %d: %w", chainID, err)
	}

	r.adapters[chainID] = adapter
	r.configs[chainID] = config
	r.metrics[chainID] = &ChainMetrics{ChainID: chainID}

	return nil
}

// Get returns an adapter for the given chain
func (r *Registry) Get(chainID ChainID) (ChainAdapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	adapter, ok := r.adapters[chainID]
	if !ok {
		return nil, ErrChainNotSupported
	}
	return adapter, nil
}

// GetConfig returns the config for a chain
func (r *Registry) GetConfig(chainID ChainID) (*ChainConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	config, ok := r.configs[chainID]
	if !ok {
		return nil, ErrChainNotSupported
	}
	return config, nil
}

// List returns all registered chain IDs
func (r *Registry) List() []ChainID {
	r.mu.RLock()
	defer r.mu.RUnlock()

	chains := make([]ChainID, 0, len(r.adapters))
	for chainID := range r.adapters {
		chains = append(chains, chainID)
	}
	return chains
}

// Unregister removes a chain adapter
func (r *Registry) Unregister(chainID ChainID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	adapter, ok := r.adapters[chainID]
	if !ok {
		return ErrChainNotSupported
	}

	if err := adapter.Close(); err != nil {
		return fmt.Errorf("failed to close adapter: %w", err)
	}

	delete(r.adapters, chainID)
	delete(r.configs, chainID)
	delete(r.metrics, chainID)

	return nil
}

// GetMetrics returns metrics for a chain
func (r *Registry) GetMetrics(chainID ChainID) (*ChainMetrics, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metrics, ok := r.metrics[chainID]
	if !ok {
		return nil, ErrChainNotSupported
	}
	return metrics, nil
}

// VerifyMessage verifies a cross-chain message
func (r *Registry) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error {
	adapter, err := r.Get(msg.SourceChain)
	if err != nil {
		return err
	}

	startTime := time.Now()
	err = adapter.VerifyMessage(ctx, msg)

	// Update metrics
	r.mu.Lock()
	metrics := r.metrics[msg.SourceChain]
	metrics.TotalVerifications++
	if err != nil {
		metrics.FailedVerifications++
		metrics.LastError = err
		metrics.LastErrorTime = time.Now()
	} else {
		metrics.SuccessfulVerifications++
	}
	metrics.AverageLatency = (metrics.AverageLatency + time.Since(startTime)) / 2
	r.mu.Unlock()

	return err
}

// Close closes all adapters
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error
	for chainID, adapter := range r.adapters {
		if err := adapter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("chain %d: %w", chainID, err))
		}
	}

	r.adapters = make(map[ChainID]ChainAdapter)
	r.configs = make(map[ChainID]*ChainConfig)
	r.metrics = make(map[ChainID]*ChainMetrics)

	if len(errs) > 0 {
		return fmt.Errorf("errors closing adapters: %v", errs)
	}
	return nil
}

// ======== Default Chain Configurations ========

// DefaultChainConfigs returns default configurations for major chains
func DefaultChainConfigs() map[ChainID]*ChainConfig {
	return map[ChainID]*ChainConfig{
		idBitcoin: {
			ChainID:               idBitcoin,
			Name:                  "Bitcoin",
			NetworkID:             0, // Mainnet
			RequiredConfirmations: 6,
			FinalityMode:          "probabilistic",
			BlockTime:             10 * time.Minute,
			TrustThreshold:        0.51, // Hashpower
			MaxClockDrift:         2 * time.Hour,
			StalenessThreshold:    1 * time.Hour,
		},
		idEthereum: {
			ChainID:               idEthereum,
			Name:                  "Ethereum",
			NetworkID:             1, // Mainnet
			RequiredConfirmations: 1, // With sync committee finality
			FinalityMode:          "epoch", // 2 epochs = 12.8 min
			BlockTime:             12 * time.Second,
			TrustThreshold:        0.67, // 2/3 sync committee
			MaxClockDrift:         30 * time.Second,
			StalenessThreshold:    15 * time.Minute,
		},
		idSolana: {
			ChainID:               idSolana,
			Name:                  "Solana",
			NetworkID:             1,
			RequiredConfirmations: 32, // 32 slots for optimistic
			FinalityMode:          "instant", // After vote confirmation
			BlockTime:             400 * time.Millisecond,
			TrustThreshold:        0.67,
			MaxClockDrift:         10 * time.Second,
			StalenessThreshold:    1 * time.Minute,
		},
		idCosmos: {
			ChainID:               idCosmos,
			Name:                  "Cosmos Hub",
			NetworkID:             4, // cosmoshub-4
			RequiredConfirmations: 1, // Instant with Tendermint
			FinalityMode:          "instant",
			BlockTime:             6 * time.Second,
			TrustThreshold:        0.67,
			MaxClockDrift:         30 * time.Second,
			StalenessThreshold:    5 * time.Minute,
		},
		idPolkadot: {
			ChainID:               idPolkadot,
			Name:                  "Polkadot",
			NetworkID:             0,
			RequiredConfirmations: 1, // GRANDPA finality
			FinalityMode:          "instant",
			BlockTime:             6 * time.Second,
			TrustThreshold:        0.67,
			MaxClockDrift:         30 * time.Second,
			StalenessThreshold:    5 * time.Minute,
		},
		idPolygon: {
			ChainID:               idPolygon,
			Name:                  "Polygon",
			NetworkID:             137, // Polygon Mainnet
			RequiredConfirmations: 256, // With heimdall checkpoints
			FinalityMode:          "epoch",
			BlockTime:             2 * time.Second,
			TrustThreshold:        0.67,
			MaxClockDrift:         30 * time.Second,
			StalenessThreshold:    10 * time.Minute,
		},
		idBSC: {
			ChainID:               idBSC,
			Name:                  "BNB Smart Chain",
			NetworkID:             56, // BSC Mainnet
			RequiredConfirmations: 15,
			FinalityMode:          "probabilistic",
			BlockTime:             3 * time.Second,
			TrustThreshold:        0.67, // 2/3 validators
			MaxClockDrift:         30 * time.Second,
			StalenessThreshold:    5 * time.Minute,
		},
		idRipple: {
			ChainID:               idRipple,
			Name:                  "XRP Ledger",
			NetworkID:             0,
			RequiredConfirmations: 1, // Federated consensus
			FinalityMode:          "instant",
			BlockTime:             4 * time.Second,
			TrustThreshold:        0.80, // 80% of UNL
			MaxClockDrift:         30 * time.Second,
			StalenessThreshold:    5 * time.Minute,
		},
		idAvalanche: {
			ChainID:               idAvalanche,
			Name:                  "Avalanche",
			NetworkID:             43114, // C-Chain
			RequiredConfirmations: 1,
			FinalityMode:          "instant", // Quasar consensus
			BlockTime:             2 * time.Second,
			TrustThreshold:        0.67,
			MaxClockDrift:         30 * time.Second,
			StalenessThreshold:    5 * time.Minute,
		},
		idArbitrum: {
			ChainID:               idArbitrum,
			Name:                  "Arbitrum One",
			NetworkID:             42161,
			RequiredConfirmations: 1, // L1 batch posting
			FinalityMode:          "epoch",
			BlockTime:             250 * time.Millisecond,
			TrustThreshold:        0.67,
			MaxClockDrift:         30 * time.Second,
			StalenessThreshold:    15 * time.Minute,
		},
		idOptimism: {
			ChainID:               idOptimism,
			Name:                  "Optimism",
			NetworkID:             10,
			RequiredConfirmations: 1,
			FinalityMode:          "epoch",
			BlockTime:             2 * time.Second,
			TrustThreshold:        0.67,
			MaxClockDrift:         30 * time.Second,
			StalenessThreshold:    15 * time.Minute,
		},
		idBase: {
			ChainID:               idBase,
			Name:                  "Base",
			NetworkID:             8453,
			RequiredConfirmations: 1,
			FinalityMode:          "epoch",
			BlockTime:             2 * time.Second,
			TrustThreshold:        0.67,
			MaxClockDrift:         30 * time.Second,
			StalenessThreshold:    15 * time.Minute,
		},
		idTron: {
			ChainID:               idTron,
			Name:                  "TRON",
			NetworkID:             728126428,
			RequiredConfirmations: 19,
			FinalityMode:          "probabilistic",
			BlockTime:             3 * time.Second,
			TrustThreshold:        0.67,
			MaxClockDrift:         30 * time.Second,
			StalenessThreshold:    5 * time.Minute,
		},
		idCardano: {
			ChainID:               idCardano,
			Name:                  "Cardano",
			NetworkID:             1, // Mainnet
			RequiredConfirmations: 2160, // ~12 hours
			FinalityMode:          "probabilistic",
			BlockTime:             20 * time.Second,
			TrustThreshold:        0.51,
			MaxClockDrift:         30 * time.Second,
			StalenessThreshold:    30 * time.Minute,
		},
		idNear: {
			ChainID:               idNear,
			Name:                  "NEAR Protocol",
			NetworkID:             1,
			RequiredConfirmations: 1,
			FinalityMode:          "instant",
			BlockTime:             1 * time.Second,
			TrustThreshold:        0.67,
			MaxClockDrift:         30 * time.Second,
			StalenessThreshold:    5 * time.Minute,
		},
		idAptos: {
			ChainID:               idAptos,
			Name:                  "Aptos",
			NetworkID:             1,
			RequiredConfirmations: 1,
			FinalityMode:          "instant",
			BlockTime:             1 * time.Second,
			TrustThreshold:        0.67,
			MaxClockDrift:         30 * time.Second,
			StalenessThreshold:    5 * time.Minute,
		},
		idSui: {
			ChainID:               idSui,
			Name:                  "Sui",
			NetworkID:             1,
			RequiredConfirmations: 1,
			FinalityMode:          "instant",
			BlockTime:             500 * time.Millisecond,
			TrustThreshold:        0.67,
			MaxClockDrift:         30 * time.Second,
			StalenessThreshold:    5 * time.Minute,
		},
		idTON: {
			ChainID:               idTON,
			Name:                  "TON",
			NetworkID:             0xFFFFFFFFFFFFFF11, // -239 as two's complement
			RequiredConfirmations: 1,
			FinalityMode:          "instant",
			BlockTime:             5 * time.Second,
			TrustThreshold:        0.67,
			MaxClockDrift:         30 * time.Second,
			StalenessThreshold:    5 * time.Minute,
		},
	}
}

// ChainName returns the name for a chain ID
func ChainName(id ChainID) string {
	names := map[ChainID]string{
		idBitcoin:   "Bitcoin",
		idEthereum:  "Ethereum",
		idSolana:    "Solana",
		idCosmos:    "Cosmos Hub",
		idPolkadot:  "Polkadot",
		idPolygon:   "Polygon",
		idBSC:       "BNB Smart Chain",
		idRipple:    "XRP Ledger",
		idAvalanche: "Avalanche",
		idArbitrum:  "Arbitrum One",
		idOptimism:  "Optimism",
		idBase:      "Base",
		idTron:      "TRON",
		idCardano:   "Cardano",
		idNear:      "NEAR Protocol",
		idAptos:     "Aptos",
		idSui:       "Sui",
		idTON:       "TON",
		idLux:       "Lux",
	}
	if name, ok := names[id]; ok {
		return name
	}
	return fmt.Sprintf("Unknown(%d)", id)
}
