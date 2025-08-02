// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import "github.com/luxfi/ids"

const (
	PlatformVMName = "platformvm" // P-Chain: Platform VM
	QuantumVMName  = "quantumvm"  // Q-Chain: Quantum VM
	XVMName        = "xvm"        // X-Chain: Exchange VM
	EVMName        = "evm"        // C-Chain: Ethereum VM
	SubnetEVMName  = "subnetevm"
	XSVMName       = "xsvm"
	AIVMName       = "aivm"       // A-Chain: AI VM
	BridgeVMName   = "bridgevm"   // B-Chain: Bridge VM
	ZKVMName       = "zkvm"       // Z-Chain: ZK VM
	MPCVMName      = "mpcvm"      // M-Chain: MPC VM
)

var (
	PlatformVMID = ids.ID{'p', 'l', 'a', 't', 'f', 'o', 'r', 'm', 'v', 'm'} // P-Chain: Platform VM
	QuantumVMID  = ids.ID{'q', 'u', 'a', 'n', 't', 'u', 'm', 'v', 'm'}      // Q-Chain: Quantum VM
	XVMID        = ids.ID{'a', 'v', 'm'}                                    // X-Chain: Exchange VM
	EVMID        = ids.ID{'e', 'v', 'm'}                                    // C-Chain: Ethereum VM
	SubnetEVMID  = ids.ID{'s', 'u', 'b', 'n', 'e', 't', 'e', 'v', 'm'}
	XSVMID       = ids.ID{'x', 's', 'v', 'm'}
	AIVMID       = ids.ID{'a', 'i', 'v', 'm'}                               // A-Chain: AI VM (native AI support)
	BridgeVMID   = ids.ID{'b', 'r', 'i', 'd', 'g', 'e', 'v', 'm'}          // B-Chain: Bridge VM (wraps M/Z)
	ZKVMID       = ids.ID{'z', 'k', 'v', 'm'}                               // Z-Chain: ZK VM
	MPCVMID      = ids.ID{'m', 'p', 'c', 'v', 'm'}                          // M-Chain: MPC VM
)

// VMName returns the name of the VM with the provided ID. If a human readable
// name isn't known, then the formatted ID is returned.
func VMName(vmID ids.ID) string {
	switch vmID {
	case PlatformVMID:
		return PlatformVMName
	case QuantumVMID:
		return QuantumVMName
	case XVMID:
		return XVMName
	case EVMID:
		return EVMName
	case SubnetEVMID:
		return SubnetEVMName
	case XSVMID:
		return XSVMName
	case AIVMID:
		return AIVMName
	case BridgeVMID:
		return BridgeVMName
	case ZKVMID:
		return ZKVMName
	case MPCVMID:
		return MPCVMName
	default:
		return vmID.String()
	}
}
