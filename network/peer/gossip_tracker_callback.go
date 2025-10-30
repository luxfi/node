// Copyright (C) 2019-2025, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"go.uber.org/zap"

	"github.com/luxfi/ids"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/log"
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
			zap.Stringer("nodeID", nodeID),
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
