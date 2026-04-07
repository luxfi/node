// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleportvm

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms"
)

var _ vms.Factory = (*Factory)(nil)

// VMID is the unique identifier for TeleportVM (T-Chain)
var VMID = ids.ID{'t', 'e', 'l', 'e', 'p', 'o', 'r', 't', 'v', 'm'}

// Factory creates new TeleportVM instances
type Factory struct{}

// New returns a new instance of the TeleportVM
func (f *Factory) New(logger log.Logger) (interface{}, error) {
	return &VM{
		// Bridge state
		pendingBridges: make(map[ids.ID]*BridgeRequest),
		chainClients:   make(map[string]ChainClient),

		// Relay state
		channels:    make(map[ids.ID]*Channel),
		messages:    make(map[ids.ID]*Message),
		pendingMsgs: make(map[ids.ID][]*Message),
		sequences:   make(map[ids.ID]uint64),

		// Oracle state
		feeds:       make(map[ids.ID]*Feed),
		feedsByName: make(map[string]ids.ID),
		pendingObs:  make(map[ids.ID][]*Observation),
		values:      make(map[ids.ID]map[uint64]*AggregatedValue),

		// Block state
		pendingBlocks: make(map[ids.ID]*Block),
	}, nil
}
