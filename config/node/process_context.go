// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import "net/netip"

type ProcessContext struct {
	// The process id of the node
	PID int `json:"pid"`
	// URI to access the node API
	// Format: [https|http]://[host]:[port]
	URI string `json:"uri"`
	// Address other nodes can use to communicate with this node
	StakingAddress netip.AddrPort `json:"stakingAddress"`
}
