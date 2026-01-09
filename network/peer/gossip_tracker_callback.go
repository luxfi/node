// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/luxfi/log"

	validators "github.com/luxfi/consensus/validator"
	"github.com/luxfi/ids"
)

var _ validators.SetCallbackListener = (*GossipTrackerCallback)(nil)

// GossipTrackerCallback synchronizes GossipTracker's validator state with the
// validator set it's registered to.
type GossipTrackerCallback struct {
	Log           log.Logger
	GossipTracker GossipTracker
}

// OnValidatorAdded adds [validatorID] to the set of validators that can be
// gossiped about
func (g *GossipTrackerCallback) OnValidatorAdded(nodeID ids.NodeID, _ uint64) {
	vdr := ValidatorID{
		NodeID: nodeID,
		TxID:   ids.Empty, // No longer provided, use empty ID
	}
	if !g.GossipTracker.AddValidator(vdr) {
		g.Log.Error("failed to add a validator",
			log.Stringer("nodeID", nodeID),
		)
	}
}

// OnValidatorRemoved removes [validatorID] from the set of validators that can
// be gossiped about.
func (g *GossipTrackerCallback) OnValidatorRemoved(nodeID ids.NodeID, _ uint64) {
	if !g.GossipTracker.RemoveValidator(nodeID) {
		g.Log.Error("failed to remove a validator",
			log.Stringer("nodeID", nodeID),
		)
	}
}

// OnValidatorLightChanged does nothing because PeerList gossip doesn't care
// about validator weights.
func (*GossipTrackerCallback) OnValidatorLightChanged(ids.NodeID, uint64, uint64) {}
