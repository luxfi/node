// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import "github.com/luxfi/ids"

// Chain IDs for the 8-chain architecture
var (
	// Core chains
	PlatformChainID = ids.Empty.Prefix(0) // P-Chain: Platform chain for validator management
	QuantumChainID  = ids.Empty.Prefix(1) // Q-Chain: Quantum chain for post-quantum finality
	
	// User-facing chains
	CChainID = ids.Empty.Prefix(2) // C-Chain: EVM-compatible smart contract chain
	XChainID = ids.Empty.Prefix(3) // X-Chain: Asset exchange chain
	
	// Genesis NFT-gated chains
	BridgeChainID = ids.Empty.Prefix(4) // B-Chain: Bridge chain for cross-chain operations
	MPCChainID    = ids.Empty.Prefix(5) // M-Chain: MPC chain for threshold signatures
	
	// Specialized opt-in chains
	AIChainID = ids.Empty.Prefix(6) // A-Chain: AI operations chain
	ZKChainID = ids.Empty.Prefix(7) // Z-Chain: Zero-knowledge proof chain
)

// ChainName returns the human-readable name for a chain ID
func ChainName(chainID ids.ID) string {
	switch chainID {
	case PlatformChainID:
		return "P-Chain"
	case QuantumChainID:
		return "Q-Chain"
	case CChainID:
		return "C-Chain"
	case XChainID:
		return "X-Chain"
	case BridgeChainID:
		return "B-Chain"
	case MPCChainID:
		return "M-Chain"
	case AIChainID:
		return "A-Chain"
	case ZKChainID:
		return "Z-Chain"
	default:
		return chainID.String()
	}
}

// IsGenesisGatedChain returns true if the chain requires Genesis NFT ownership
func IsGenesisGatedChain(chainID ids.ID) bool {
	return chainID == BridgeChainID || chainID == MPCChainID
}

// IsCoreChain returns true if the chain is required for all validators
func IsCoreChain(chainID ids.ID) bool {
	return chainID == PlatformChainID || chainID == QuantumChainID
}

// IsOptInChain returns true if the chain is optional for validators
func IsOptInChain(chainID ids.ID) bool {
	return chainID == AIChainID || chainID == ZKChainID ||
		chainID == BridgeChainID || chainID == MPCChainID ||
		chainID == CChainID || chainID == XChainID
}