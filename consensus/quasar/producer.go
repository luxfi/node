// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"context"

	qcert "github.com/luxfi/consensus/protocol/quasar"
)

// Subject is the finalized-block position a cert must certify — the producer's
// input at a checkpoint. Mirrors Checkpoint (the verify side) so producer and
// verifier bind the SAME tuple.
type Subject struct {
	ChainID   uint32
	Epoch     uint64
	Height    uint64
	Round     uint32
	BlockID   [32]byte
	StateRoot [32]byte
}

// subjectFrom derives the producer Subject from this gate's chain id and a
// finalized Checkpoint, so producer and verifier bind the SAME tuple.
func (g *Gate) subjectFrom(cp Checkpoint) Subject {
	return Subject{
		ChainID:   g.cfg.ChainID,
		Epoch:     cp.Epoch,
		Height:    cp.Height,
		Round:     cp.Round,
		BlockID:   cp.BlockID,
		StateRoot: cp.StateRoot,
	}
}

// Producer is the committee cert-signing service contract (the per-validator
// "pulsard" committee). At a checkpoint, a producing validator calls Produce to
// obtain the QuasarCert over the finalized subject, then gossips it so peers can
// verify and store it (via a CertStore).
//
// SCAFFOLDING — this is the seam, not the service. This milestone wires the
// VERIFY half (gate.go) and this interface. luxd ships with a nil Producer
// (verify-only): a node VERIFIES certs it receives but does not itself produce
// them. A nil Producer is the correct default — most of the rollout window is
// verify-only, and the producer is brought up before activation is forward-dated.
//
// Implementation path for the follow-on:
//
//   - github.com/luxfi/consensus/protocol/quasar already defines the
//     producer-side abstractions: PWitnessProducer / QWitnessProducer /
//     ZWitnessProducer + NewWitnessSet, and ComposeDualPQEvidence. The concrete
//     committee signer implements Producer over those.
//   - The signer needs the live Pulsar key share + nonce pool + offline
//     preprocessing + one-round sign + verify-before-gossip + nonce-erase (the
//     no-reconstruct hyperball signer), which lands with pulsar v1.7.1.
//   - REQUIRED CONSENSUS EXPORT: the ConsensusCert envelope + per-leg payload
//     ENCODERS are package-private in consensus v1.29.0 (only the verifiers are
//     exported). An external producer — and any end-to-end "valid cert verifies
//     through the gate" test — needs those encoders exported (a small, additive
//     consensus change). The verify path here needs no such export: it consumes
//     a fully-formed *ConsensusCert.
type Producer interface {
	Produce(ctx context.Context, subject Subject) (*qcert.ConsensusCert, error)
}

// MaybeProduce is the checkpoint producer-request site. It is nil-safe and
// activation-aware so the accept hook can call it unconditionally: a nil gate,
// dormant activation, a non-checkpoint height, or a nil producer all short-
// circuit to (nil, nil) — the verify-only default. When a producer IS wired and
// the checkpoint is live, it requests the cert; the caller gossips/stores it.
//
// This keeps producer cadence and verify cadence on ONE definition (g.IsCheckpoint),
// so producer and verifier can never disagree on which heights carry certs.
func (g *Gate) MaybeProduce(ctx context.Context, producer Producer, cp Checkpoint) (*qcert.ConsensusCert, error) {
	if g == nil || producer == nil {
		return nil, nil
	}
	if !g.Activated(cp.Height) || !g.IsCheckpoint(cp.Height) {
		return nil, nil
	}
	return producer.Produce(ctx, g.subjectFrom(cp))
}
