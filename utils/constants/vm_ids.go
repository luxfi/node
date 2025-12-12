// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import "github.com/luxfi/ids"

const (
	PlatformVMName   = "platformvm"   // P-Chain: Platform/Validators
	XVMName          = "xvm"          // X-Chain: UTXO Exchange
	EVMName          = "evm"          // C-Chain: EVM Smart Contracts
	XSVMName         = "xsvm"         // Cross-Subnet VM
	QuantumVMName    = "quantumvm"    // Q-Chain: Quantum-resistant security
	AIVMName         = "aivm"         // A-Chain: AI Virtual Machine
	BridgeVMName     = "bridgevm"     // B-Chain: Bridge/Cross-chain
	ThresholdVMName  = "thresholdvm"  // T-Chain: Threshold signatures
	ZKVMName         = "zkvm"         // Z-Chain: Zero-Knowledge proofs
	GraphVMName      = "graphvm"      // G-Chain: GraphQL/DGraph unified data layer
	DexVMName        = "dexvm"        // D-Chain: Decentralized Exchange
)

var (
	PlatformVMID    = ids.ID{'p', 'l', 'a', 't', 'f', 'o', 'r', 'm', 'v', 'm'}
	ExchangeVMID    = ids.ID{'a', 'v', 'm'} // X-Chain: UTXO Exchange
	XVMID           = ExchangeVMID         // Alias for ExchangeVMID
	ContractVMID    = ids.ID{'e', 'v', 'm'} // C-Chain: EVM Smart Contracts
	EVMID           = ContractVMID         // Alias for ContractVMID
	XSVMID          = ids.ID{'x', 's', 'v', 'm'}
	QuantumVMID     = ids.ID{'q', 'u', 'a', 'n', 't', 'u', 'm', 'v', 'm'}
	QVMID           = QuantumVMID                // Alias for QuantumVMID
	AttestationVMID = ids.ID{'a', 'i', 'v', 'm'} // A-Chain: Attestation/AI VM
	AIVMID          = AttestationVMID            // Alias for AttestationVMID
	BridgeVMID      = ids.ID{'b', 'r', 'i', 'd', 'g', 'e', 'v', 'm'}
	ThresholdVMID   = ids.ID{'t', 'h', 'r', 'e', 's', 'h', 'o', 'l', 'd', 'v', 'm'}
	ZKVMID          = ids.ID{'z', 'k', 'v', 'm'}
	GraphVMID       = ids.ID{'g', 'r', 'a', 'p', 'h', 'v', 'm'}
	DexVMID         = ids.ID{'d', 'e', 'x', 'v', 'm'} // D-Chain: Decentralized Exchange
)

// VMName returns the name of the VM with the provided ID. If a human readable
// name isn't known, then the formatted ID is returned.
func VMName(vmID ids.ID) string {
	switch vmID {
	case PlatformVMID:
		return PlatformVMName
	case XVMID:
		return XVMName
	case EVMID:
		return EVMName
	case XSVMID:
		return XSVMName
	case QuantumVMID:
		return QuantumVMName
	case AIVMID:
		return AIVMName
	case BridgeVMID:
		return BridgeVMName
	case ThresholdVMID:
		return ThresholdVMName
	case ZKVMID:
		return ZKVMName
	case GraphVMID:
		return GraphVMName
	case DexVMID:
		return DexVMName
	default:
		return vmID.String()
	}
}
