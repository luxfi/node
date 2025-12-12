// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package qvm provides a thin wrapper around consensus/quasar for backward compatibility.
// The actual Quasar implementation is in github.com/luxfi/node/consensus/quasar.
package qvm

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/consensus/quasar"
	"github.com/luxfi/node/vms/quantumvm/quantum"
)

// Re-export types from consensus/quasar for backward compatibility
type (
	Quasar          = quasar.Quasar
	QuantumFinality = quasar.QuantumFinality
	ValidatorState  = quasar.ValidatorState
	FinalityEvent   = quasar.FinalityEvent
	QuasarStats     = quasar.QuasarStats
	PChainProvider  = quasar.PChainProvider
)

// Re-export errors
var (
	ErrQuasarNotStarted     = quasar.ErrQuasarNotStarted
	ErrPChainNotConnected   = quasar.ErrPChainNotConnected
	ErrQChainNotConnected   = quasar.ErrQChainNotConnected
	ErrRingtailNotConnected = quasar.ErrRingtailNotConnected
	ErrInsufficientWeight   = quasar.ErrInsufficientWeight
	ErrInsufficientSigners  = quasar.ErrInsufficientSigners
	ErrFinalityFailed       = quasar.ErrFinalityFailed
	ErrBLSFailed            = quasar.ErrBLSFailed
	ErrRingtailFailed       = quasar.ErrRingtailFailed
)

// NewQuasar creates a new Quasar instance for use with quantumvm
func NewQuasar(log log.Logger, threshold int, quorumNum, quorumDen uint64) (*Quasar, error) {
	return quasar.NewQuasar(log, threshold, quorumNum, quorumDen)
}

// quantumSignerAdapter adapts the quantum.QuantumSigner to quasar.QuantumSignerFallback
type quantumSignerAdapter struct {
	signer *quantum.QuantumSigner
	key    *quantum.RingtailKey
}

// SignMessage implements quasar.QuantumSignerFallback
func (a *quantumSignerAdapter) SignMessage(msg []byte) ([]byte, error) {
	// Generate key if not cached
	if a.key == nil {
		var err error
		a.key, err = a.signer.GenerateRingtailKey()
		if err != nil {
			return nil, err
		}
	}

	sig, err := a.signer.Sign(msg, a.key)
	if err != nil {
		return nil, err
	}
	return sig.Signature, nil
}

// ConnectQuantumSigner connects the quantum signer as a fallback to the Quasar
func ConnectQuantumSigner(q *Quasar, signer *quantum.QuantumSigner) {
	adapter := &quantumSignerAdapter{signer: signer}
	q.ConnectQuantumFallback(adapter)
}

// InitializeRingtailFromValidators initializes Ringtail from ValidatorState slice
func InitializeRingtailFromValidators(q *Quasar, validators []ValidatorState) error {
	nodeIDs := make([]ids.NodeID, 0, len(validators))
	for _, v := range validators {
		if v.Active {
			nodeIDs = append(nodeIDs, v.NodeID)
		}
	}
	return q.InitializeRingtail(nodeIDs)
}

// ConnectRingtail connects a Ringtail coordinator from consensus/quasar
func ConnectRingtail(q *Quasar, rc *quasar.RingtailCoordinator) {
	q.ConnectRingtail(rc)
}

// NewRingtailCoordinator creates a new Ringtail coordinator
func NewRingtailCoordinator(log log.Logger, numParties, threshold int) (*quasar.RingtailCoordinator, error) {
	return quasar.NewRingtailCoordinator(log, quasar.RingtailConfig{
		NumParties: numParties,
		Threshold:  threshold,
	})
}
