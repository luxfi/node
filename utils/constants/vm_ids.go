// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import "github.com/luxfi/ids"

const (
	QuantumVMName  = "quantumvm" // Q-Chain VM (formerly PlatformVM)
	PlatformVMName = "quantumvm" // Deprecated: Use QuantumVMName
	XVMName        = "xvm"
	EVMName        = "evm"
	SubnetEVMName  = "subnetevm"
	XSVMName       = "xsvm"
)

var (
	QuantumVMID  = ids.ID{'q', 'u', 'a', 'n', 't', 'u', 'm', 'v', 'm'}      // Q-Chain VM
	PlatformVMID = ids.ID{'q', 'u', 'a', 'n', 't', 'u', 'm', 'v', 'm'}      // Deprecated: Use QuantumVMID
	XVMID        = ids.ID{'a', 'v', 'm'}
	EVMID        = ids.ID{'e', 'v', 'm'}
	SubnetEVMID  = ids.ID{'s', 'u', 'b', 'n', 'e', 't', 'e', 'v', 'm'}
	XSVMID       = ids.ID{'x', 's', 'v', 'm'}
)

// VMName returns the name of the VM with the provided ID. If a human readable
// name isn't known, then the formatted ID is returned.
func VMName(vmID ids.ID) string {
	switch vmID {
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
	default:
		return vmID.String()
	}
}
