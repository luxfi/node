// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// Logger logs validator set changes
type Logger struct {
	log      log.Logger
	subnetID ids.ID
	nodeID   ids.NodeID
}

// NewLogger returns a new validator set logger
func NewLogger(log log.Logger, subnetID ids.ID, nodeID ids.NodeID) *Logger {
	return &Logger{
		log:      log,
		subnetID: subnetID,
		nodeID:   nodeID,
	}
}

// OnValidatorAdded implements SetCallbackListener
func (l *Logger) OnValidatorAdded(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
	l.log.Debug("validator added",
		"subnetID", l.subnetID,
		"nodeID", nodeID,
		"txID", txID,
		"weight", weight,
	)
}

// OnValidatorRemoved implements SetCallbackListener
func (l *Logger) OnValidatorRemoved(nodeID ids.NodeID, weight uint64) {
	l.log.Debug("validator removed",
		"subnetID", l.subnetID,
		"nodeID", nodeID,
		"weight", weight,
	)
}

// OnValidatorWeightChanged implements SetCallbackListener
func (l *Logger) OnValidatorWeightChanged(nodeID ids.NodeID, oldWeight, newWeight uint64) {
	l.log.Debug("validator weight changed",
		"subnetID", l.subnetID,
		"nodeID", nodeID,
		"oldWeight", oldWeight,
		"newWeight", newWeight,
	)
}