// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import "github.com/luxfi/ids"

const (
	PlatformVMName   = "platformvm"   // D-Chain: Platform/Validators
	XVMName          = "xvm"          // X-Chain: UTXO Exchange
	EVMName          = "evm"          // C-Chain: EVM Smart Contracts
	XSVMName         = "xsvm"         // Cross-Subnet VM
	QVMName          = "qvm"          // Q-Chain: Quantum-resistant
	AIVMName         = "aivm"         // A-Chain: AI Virtual Machine
	BridgeVMName     = "bridgevm"     // B-Chain: Bridge/Cross-chain
	ThresholdVMName  = "thresholdvm"  // T-Chain: Threshold signatures
	ZKVMName         = "zkvm"         // Z-Chain: Zero-Knowledge proofs
)

var (
	PlatformVMID  = ids.ID{'p', 'l', 'a', 't', 'f', 'o', 'r', 'm', 'v', 'm'}
	XVMID         = ids.ID{'a', 'v', 'm'}
	EVMID         = ids.ID{'e', 'v', 'm'}
	XSVMID        = ids.ID{'x', 's', 'v', 'm'}
	QVMID         = ids.ID{'q', 'v', 'm'}
	AIVMID        = ids.ID{'a', 'i', 'v', 'm'}
	BridgeVMID    = ids.ID{'b', 'r', 'i', 'd', 'g', 'e', 'v', 'm'}
	ThresholdVMID = ids.ID{'t', 'h', 'r', 'e', 's', 'h', 'o', 'l', 'd', 'v', 'm'}
	ZKVMID        = ids.ID{'z', 'k', 'v', 'm'}
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
	case QVMID:
		return QVMName
	case AIVMID:
		return AIVMName
	case BridgeVMID:
		return BridgeVMName
	case ThresholdVMID:
		return ThresholdVMName
	case ZKVMID:
		return ZKVMName
	default:
		return vmID.String()
	}
}
