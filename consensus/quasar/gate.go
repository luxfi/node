// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package quasar is the node-side integration of the luxfi/consensus Quasar
// post-quantum finality-certificate layer.
//
// luxd finalizes blocks on the classical Snow/Avalanche path (fast, every
// block). On top, at CHECKPOINTS (epoch boundaries — NOT every block), a sampled
// committee produces a QuasarCert over the finalized digest and validators
// VERIFY it. This package wires the VERIFY half: it consumes
// github.com/luxfi/consensus/protocol/quasar.VerifyConsensusCert as an OPTIONAL,
// FORWARD-DATED, DORMANT-BY-DEFAULT check in the block-accept path.
//
// # Safety contract
//
// The forward-dated dormant activation is the whole reason this package has the
// shape it does:
//
//   - Pre-activation (the default): VerifyAccepted is a pure no-op. Classical
//     Snow finality is UNCHANGED. A nil *Gate, a zero Gate, or an unset
//     activation height all mean "dormant" — zero behavior change.
//   - Post-activation (owner sets Activation.Height to a real, forward-dated
//     height): at every checkpoint height the gate REQUIRES a valid QuasarCert
//     bound to the just-finalized block and FAILS CLOSED — a missing or invalid
//     cert returns an error from Accept(), halting the chain rather than
//     finalizing a checkpoint without post-quantum evidence.
//
// Activation is therefore a deliberate switch the owner flips only AFTER the
// cert PRODUCER (the per-validator committee signer) is live and certs flow at
// the checkpoint cadence — otherwise every checkpoint would halt. See
// producer.go.
//
// # Default posture
//
// HYBRID_PQ = Beam(BLS) ∧ Pulsar (standard FIPS-204 threshold ML-DSA), at
// checkpoint cadence. Per the measured policy-tier benchmarks, Pulsar verify
// (~140µs) is cheaper than BLS itself and the cert is compact (~27KB) — the
// right default production finality posture. STRICT_DUAL_PQ (∧ Corona) and the
// POLARIS tiers (∧ Magnetar) are configurable for stricter mainnet finality.
package quasar

import (
	"fmt"
	"time"

	qcert "github.com/luxfi/consensus/protocol/quasar"
)

// ActivationConfig is the forward-dated activation switch.
//
// The zero value is DORMANT: Height == 0 means "never activate" and the gate is
// a no-op for every block. This mirrors the genesis upgrade-timestamp discipline
// (a far-future / unset activation point cannot affect live finality).
type ActivationConfig struct {
	// Height is the block height at and above which PQ-finality verification is
	// enforced at checkpoints. 0 == dormant (never).
	Height uint64

	// Time, when non-zero, ADDITIONALLY requires the wall-clock activation
	// moment to have passed before enforcement begins (defence in depth on the
	// forward-dating). Zero == height-only gating.
	Time time.Time
}

// dormant reports whether the activation is unset (the default — never enforce).
func (a ActivationConfig) dormant() bool { return a.Height == 0 }

// active reports whether enforcement is live for a block at the given height and
// the given wall-clock now. Dormant activation is never active.
func (a ActivationConfig) active(height uint64, now time.Time) bool {
	if a.Height == 0 {
		return false
	}
	if height < a.Height {
		return false
	}
	if !a.Time.IsZero() && now.Before(a.Time) {
		return false
	}
	return true
}

// DefaultCheckpointInterval is the default checkpoint cadence in blocks. PQ
// certs ride epoch-boundary checkpoints, never every block (Magnetar sign is
// checkpoint-only; even the cheap Pulsar sign is checkpoint cadence). The owner
// overrides this to match the producer's cadence at activation time.
const DefaultCheckpointInterval uint64 = 256

// DefaultMode is the default Quasar posture: HYBRID_PQ (Beam ∧ Pulsar).
const DefaultMode = qcert.PolicyHybridPQCheckpoint

// Config is the node-surfaced PQ-finality configuration. Its zero value is
// dormant + HYBRID_PQ + default cadence.
type Config struct {
	// ChainID is THIS chain's numeric identifier (the sovereign/EVM chain id),
	// bound into every cert and checked against it. A per-chain constant sourced
	// from chain config at gate construction — NOT pulled from a block, because
	// the proposervm layer carries the 32-byte validator-set id, not the numeric
	// chain id. Inert while dormant.
	ChainID uint32

	// Activation is the forward-dated dormant switch. Zero => dormant.
	Activation ActivationConfig

	// Mode is the Quasar evidence posture. Zero => DefaultMode (HYBRID_PQ).
	Mode qcert.QuasarEvidenceMode

	// MLDSAParam selects the ML-DSA parameter set for the Pulsar leg. 0 =>
	// ML-DSA-65 (the consensus default).
	MLDSAParam uint8

	// Threshold is the BFT quorum floor (minimum aggregate signer weight) every
	// leg's evidence must establish.
	Threshold uint64

	// CheckpointInterval is the checkpoint cadence in blocks. 0 =>
	// DefaultCheckpointInterval.
	CheckpointInterval uint64
}

// Checkpoint is the finalized-block position the accept hook hands the gate. It
// is the binding the cert must match (anti-replay): a valid cert for a DIFFERENT
// block must never satisfy THIS checkpoint. The chain id is gate-level config,
// not a per-block field.
type Checkpoint struct {
	Epoch     uint64
	Height    uint64
	Round     uint32
	BlockID   [32]byte
	StateRoot [32]byte
}

// Gate enforces (or, dormant, ignores) PQ-finality at checkpoints. It is the
// single node-side seam between the classical accept path and the consensus
// Quasar verifier.
type Gate struct {
	cfg        Config
	policy     *qcert.QuasarEvidencePolicy
	store      CertStore
	validators ValidatorSetProvider
}

// NewGate constructs a Gate. A Gate is meaningful even with a dormant Config:
// VerifyAccepted is a no-op until Activation.Height is set. store and validators
// are only consulted post-activation at checkpoints.
func NewGate(cfg Config, store CertStore, validators ValidatorSetProvider) *Gate {
	mode := cfg.Mode
	if mode == 0 {
		mode = DefaultMode
	}
	if cfg.CheckpointInterval == 0 {
		cfg.CheckpointInterval = DefaultCheckpointInterval
	}
	cfg.Mode = mode
	return &Gate{
		cfg:        cfg,
		policy:     qcert.NewQuasarEvidencePolicy(mode, cfg.MLDSAParam, cfg.Threshold),
		store:      store,
		validators: validators,
	}
}

// VerifyAccepted is the accept-path hook and the SAFETY BOUNDARY.
//
//   - g == nil OR dormant activation => returns nil immediately. This is the
//     default and guarantees classical Snow finality is unchanged.
//   - height below activation, or activation time not yet reached => nil.
//   - not a checkpoint height => nil (certs ride checkpoints only).
//   - checkpoint, activated => REQUIRE a valid cert bound to this block; FAIL
//     CLOSED. A missing, mis-bound, or invalid cert is an error (the caller
//     returns it from Accept, halting rather than finalizing without PQ
//     evidence).
//
// It is intentionally nil-safe so the proposervm hook can call
// vm.quasarGate.VerifyAccepted(...) unconditionally with a nil gate.
func (g *Gate) VerifyAccepted(cp Checkpoint) error {
	if g == nil || g.cfg.Activation.dormant() {
		return nil
	}
	if !g.cfg.Activation.active(cp.Height, time.Now()) {
		return nil
	}
	if !g.isCheckpoint(cp.Height) {
		return nil
	}

	cert, ok := g.store.Lookup(g.cfg.ChainID, cp.Height, cp.BlockID)
	if !ok || cert == nil {
		return fmt.Errorf("%w: chain=%d height=%d block=%x", ErrFinalityCertMissing, g.cfg.ChainID, cp.Height, cp.BlockID[:8])
	}
	if err := bindCheck(cert, g.cfg.ChainID, cp); err != nil {
		return err
	}

	vs, err := g.validators.ValidatorSet(g.cfg.ChainID, cert.Epoch)
	if err != nil {
		return fmt.Errorf("%w: chain=%d epoch=%d: %v", ErrValidatorSetUnavailable, g.cfg.ChainID, cert.Epoch, err)
	}
	if err := qcert.VerifyConsensusCert(policyStore{policy: g.policy}, vs, cert); err != nil {
		return fmt.Errorf("%w: chain=%d height=%d: %v", ErrFinalityCertInvalid, g.cfg.ChainID, cp.Height, err)
	}
	return nil
}

// Activated reports whether enforcement is live for a block at the given height
// and the current wall clock. Used by the producer-request site to decide
// whether a cert is needed at a checkpoint.
func (g *Gate) Activated(height uint64) bool {
	if g == nil {
		return false
	}
	return g.cfg.Activation.active(height, time.Now())
}

// IsCheckpoint reports whether the given height is a checkpoint under the gate's
// configured cadence. Exported so the producer-request site shares ONE cadence
// definition with the verify path (no second source of truth).
func (g *Gate) IsCheckpoint(height uint64) bool {
	if g == nil {
		return false
	}
	return g.isCheckpoint(height)
}

func (g *Gate) isCheckpoint(height uint64) bool {
	iv := g.cfg.CheckpointInterval
	if iv == 0 {
		iv = DefaultCheckpointInterval
	}
	return height%iv == 0
}

// bindCheck pins the cert to the actual finalized block. Without this, a valid
// cert produced for a different (chain, height, block) could be replayed to
// satisfy this checkpoint. VerifyConsensusCert checks the cert's INTERNAL
// consistency and the validator-set/policy binding; bindCheck adds the external
// binding to THIS node's finalized position.
func bindCheck(cert *qcert.ConsensusCert, chainID uint32, cp Checkpoint) error {
	if cert.ChainID != chainID {
		return fmt.Errorf("%w: cert chain %d != finalized chain %d", ErrFinalityCertMismatch, cert.ChainID, chainID)
	}
	if cert.Height != cp.Height {
		return fmt.Errorf("%w: cert height %d != finalized height %d", ErrFinalityCertMismatch, cert.Height, cp.Height)
	}
	if cert.BlockHash != cp.BlockID {
		return fmt.Errorf("%w: cert block hash != finalized block id", ErrFinalityCertMismatch)
	}
	// StateRoot binds only when the cert commits it. A zero cert StateRoot means
	// "committed transitively through BlockHash" (the envelope spec), so it is
	// not cross-checked.
	var zero [32]byte
	if cert.StateRoot != zero && cert.StateRoot != cp.StateRoot {
		return fmt.Errorf("%w: cert state root != finalized state root", ErrFinalityCertMismatch)
	}
	return nil
}
