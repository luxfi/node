// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import "github.com/luxfi/node/ids"

const (
	PlatformVMName = "platformvm"
	AVMName        = "avm"
	EVMName        = "evm"
	SubnetEVMName  = "subnetevm"
	XSVMName       = "xsvm"
	AIVMName       = "aivm"
	BridgeVMName   = "bridgevm"
	ZKVMName       = "zkvm"
)

var (
	PlatformVMID = ids.ID{'p', 'l', 'a', 't', 'f', 'o', 'r', 'm', 'v', 'm'}
	AVMID        = ids.ID{'a', 'v', 'm'}
	EVMID        = ids.ID{'e', 'v', 'm'}
	SubnetEVMID  = ids.ID{'s', 'u', 'b', 'n', 'e', 't', 'e', 'v', 'm'}
	XSVMID       = ids.ID{'x', 's', 'v', 'm'}
	AIVMID       = ids.ID{'a', 'i', 'v', 'm'}
	BridgeVMID   = ids.ID{'b', 'r', 'i', 'd', 'g', 'e', 'v', 'm'}
	ZKVMID       = ids.ID{'z', 'k', 'v', 'm'}
)

// VMName returns the name of the VM with the provided ID. If a human readable
// name isn't known, then the formatted ID is returned.
func VMName(vmID ids.ID) string {
	switch vmID {
	case PlatformVMID:
		return PlatformVMName
	case AVMID:
		return AVMName
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
	default:
		return vmID.String()
	}
}
