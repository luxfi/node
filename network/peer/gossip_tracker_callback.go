// Copyright (C) 2019-2023, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"go.uber.org/zap"

	"github.com/luxfi/node/chain/validators"
	"github.com/luxfi/ids"
	"github.com/luxfi/crypto/bls"
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
func (g *GossipTrackerCallback) OnValidatorAdded(
	nodeID ids.NodeID,
	_ *bls.PublicKey,
	txID ids.ID,
	_ uint64,
) {
	vdr := ValidatorID{
		NodeID: nodeID,
		TxID:   txID,
	}
	if !g.GossipTracker.AddValidator(vdr) {
		g.Log.Error("failed to add a validator",
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("txID", txID),
		)
	}
}

// OnValidatorRemoved removes [validatorID] from the set of validators that can
// be gossiped about.
func (g *GossipTrackerCallback) OnValidatorRemoved(nodeID ids.NodeID, _ uint64) {
	if !g.GossipTracker.RemoveValidator(nodeID) {
		g.Log.Error("failed to remove a validator",
			zap.Stringer("nodeID", nodeID),
		)
	}
}

// OnValidatorWeightChanged does nothing because PeerList gossip doesn't care
// about validator weights.
func (*GossipTrackerCallback) OnValidatorWeightChanged(ids.NodeID, uint64, uint64) {}
