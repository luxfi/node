// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"fmt"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/quasar"
	"github.com/luxfi/node/genesis/builder"
)

// initQuasar creates and starts the Quasar hybrid finality engine.
// Quasar binds P-Chain BLS finality with Q-Chain Corona threshold
// signatures for post-quantum secure block finality.
//
// Requires Q-Chain to be present in genesis. If absent, returns an error
// and the caller logs a warning — the node operates without hybrid finality.
func (n *Node) initQuasar() error {
	createQVMTx, err := builder.VMGenesis(n.Config.GenesisBytes, constants.QuantumVMID)
	if err != nil {
		return fmt.Errorf("Q-Chain not in genesis: %w", err)
	}
	qChainID := createQVMTx.ID()

	// BLS quorum: 2/3 validator weight required
	// Corona threshold: 2 (minimum for threshold signing)
	q, err := quasar.NewQuasar(n.Log, 2, 2, 3)
	if err != nil {
		return fmt.Errorf("quasar create: %w", err)
	}

	// Wire P-Chain provider — delivers validator state and finality events.
	// This is a stub adapter returning only the local node as validator.
	// Wire to PlatformVM's native finality subscription for production
	// multi-validator consensus.
	provider := &pChainProvider{
		nodeID: n.ID,
		finCh:  make(chan quasar.FinalityEvent, 64),
	}
	q.ConnectPChain(provider)
	n.Log.Warn("quasar using stub validator provider — wire to PlatformVM for production")

	// Wire quantum fallback signer using the node's BLS key.
	// Satisfies the Start() precondition (quantumFallback != nil).
	// Corona threshold signing supersedes this once initialized.
	q.ConnectQuantumFallback(&blsQuantumFallback{
		signer: n.Config.StakingSigningKey,
	})

	if err := q.Start(context.Background()); err != nil {
		return fmt.Errorf("quasar start: %w", err)
	}

	n.Quasar = q
	n.Log.Info("quasar hybrid finality engine started",
		"qChainID", qChainID,
		"quorum", "2/3",
		"threshold", 2,
	)
	return nil
}

// pChainProvider is a minimal PChainProvider adapter.
// Provides initial wiring for Quasar until PlatformVM exposes
// a native finality subscription.
type pChainProvider struct {
	nodeID ids.NodeID
	finCh  chan quasar.FinalityEvent
}

func (p *pChainProvider) GetFinalizedHeight() uint64 { return 0 }

func (p *pChainProvider) GetValidators(_ uint64) ([]quasar.ValidatorState, error) {
	return []quasar.ValidatorState{{
		NodeID: p.nodeID,
		Weight: 1,
		Active: true,
	}}, nil
}

func (p *pChainProvider) SubscribeFinality() <-chan quasar.FinalityEvent {
	return p.finCh
}

// blsQuantumFallback wraps the node's BLS signer to satisfy
// quasar.QuantumSignerFallback.
type blsQuantumFallback struct {
	signer bls.Signer
}

func (f *blsQuantumFallback) SignMessage(msg []byte) ([]byte, error) {
	sig, err := f.signer.Sign(msg)
	if err != nil {
		return nil, fmt.Errorf("BLS sign: %w", err)
	}
	return bls.SignatureToBytes(sig), nil
}
