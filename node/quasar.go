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
	nodevalidators "github.com/luxfi/validators"
)

// initQuasar creates and starts the Quasar hybrid finality engine.
// Quasar binds P-Chain BLS finality with Q-Chain Ringtail threshold
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
	// Ringtail threshold: 2 (minimum for threshold signing)
	q, err := quasar.NewQuasar(n.Log, 2, 2, 3)
	if err != nil {
		return fmt.Errorf("quasar create: %w", err)
	}

	// Wire P-Chain provider — delivers validator state from PlatformVM's
	// validator manager (n.vdrs is populated by initValidatorSets() from
	// genesis + on-chain stakers). Finality events are emitted by
	// PChainAcceptedSubscriber once block acceptance is wired; for now the
	// channel exists but is not driven from outside (Run loop blocks on it).
	provider := &pChainProvider{
		nodeID: n.ID,
		vdrs:   n.vdrs,
		finCh:  make(chan quasar.FinalityEvent, 64),
	}
	q.ConnectPChain(provider)

	// Wire quantum fallback signer using the node's BLS key.
	// Satisfies the Start() precondition (quantumFallback != nil).
	// Ringtail threshold signing supersedes this once initialized.
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

// pChainProvider adapts the node's PlatformVM-backed validator manager to
// quasar.PChainProvider. GetValidators reads the live primary-network set
// (BLS pubkey + light/weight) so Quasar's t-of-n threshold tracks the real
// staker set as it changes. Finality events are pushed via finCh by the
// block-acceptance bridge (wired separately).
type pChainProvider struct {
	nodeID ids.NodeID
	vdrs   nodevalidators.Manager
	finCh  chan quasar.FinalityEvent
}

func (p *pChainProvider) GetFinalizedHeight() uint64 { return 0 }

func (p *pChainProvider) GetValidators(_ uint64) ([]quasar.ValidatorState, error) {
	if p.vdrs == nil {
		return nil, fmt.Errorf("validator manager not initialized")
	}
	vmap := p.vdrs.GetMap(constants.PrimaryNetworkID)
	out := make([]quasar.ValidatorState, 0, len(vmap))
	for nodeID, v := range vmap {
		// PublicKey is already serialized BLS bytes from validators.GetValidatorOutput.
		out = append(out, quasar.ValidatorState{
			NodeID:    nodeID,
			Weight:    v.Weight,
			BLSPubKey: v.PublicKey,
			Active:    true,
		})
	}
	return out, nil
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
