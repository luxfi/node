// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"context"
	"errors"
	"testing"
	"time"

	qcert "github.com/luxfi/consensus/protocol/quasar"
)

// vsRoot is a fixed committed-validator-set root used across the tests.
var vsRoot = [48]byte{0x11, 0x22, 0x33, 0x44}

// testValidators is a ValidatorSet whose Root matches vsRoot, with non-empty
// (placeholder) HYBRID_PQ keys. The delegation test never reaches signature
// math, so the key bytes need only be non-empty.
func testValidators() *ValidatorSet {
	return NewValidatorSet(vsRoot, 1, []byte("bls-agg-key"), []byte("pulsar-group-key"))
}

// newGate builds a gate with a tight cadence (checkpoint every 10 blocks) and
// the given forward-dated activation height. interval 10 keeps the heights in
// the tests readable.
func newGate(activationHeight uint64, store CertStore) *Gate {
	return NewGate(Config{
		ChainID:            1337,
		Activation:         ActivationConfig{Height: activationHeight},
		Mode:               DefaultMode, // HYBRID_PQ
		Threshold:          100,
		CheckpointInterval: 10,
	}, store, StaticValidatorSetProvider{Set: testValidators()})
}

func checkpointAt(height uint64) Checkpoint {
	return Checkpoint{
		Epoch:   1,
		Height:  height,
		BlockID: [32]byte{0xab, 0xcd, 0xef},
	}
}

// TestDormantIsNoop — the default (Activation.Height == 0) is a pure no-op even
// at a checkpoint height with a poisoned store. This is the core safety
// property: pre-activation, classical Snow finality is unchanged.
func TestDormantIsNoop(t *testing.T) {
	store := NewMemCertStore()
	g := NewGate(Config{CheckpointInterval: 10}, store, StaticValidatorSetProvider{Set: testValidators()})
	// height 10 is a checkpoint; no cert exists; yet dormant => nil.
	if err := g.VerifyAccepted(checkpointAt(10)); err != nil {
		t.Fatalf("dormant gate must be a no-op, got %v", err)
	}
	if g.Activated(10) {
		t.Fatal("dormant gate must never report Activated")
	}
}

// TestNilGateIsNoop — a nil *Gate is the wire-it-but-leave-it-off default; the
// proposervm hook calls VerifyAccepted on a possibly-nil gate.
func TestNilGateIsNoop(t *testing.T) {
	var g *Gate
	if err := g.VerifyAccepted(checkpointAt(10)); err != nil {
		t.Fatalf("nil gate must be a no-op, got %v", err)
	}
	if g.Activated(10) || g.IsCheckpoint(10) {
		t.Fatal("nil gate must report neither Activated nor IsCheckpoint")
	}
}

// TestBelowActivationIsNoop — activated at height 100 but the block is at 10:
// below the forward-dated height => nil, even at a checkpoint with no cert.
func TestBelowActivationIsNoop(t *testing.T) {
	g := newGate(100, NewMemCertStore())
	if err := g.VerifyAccepted(checkpointAt(10)); err != nil {
		t.Fatalf("below activation must be a no-op, got %v", err)
	}
}

// TestActivationTimeNotReached — height is past the activation height but the
// forward-dated wall-clock moment has not arrived => still dormant.
func TestActivationTimeNotReached(t *testing.T) {
	g := NewGate(Config{
		Activation:         ActivationConfig{Height: 10, Time: time.Now().Add(time.Hour)},
		CheckpointInterval: 10,
	}, NewMemCertStore(), StaticValidatorSetProvider{Set: testValidators()})
	if err := g.VerifyAccepted(checkpointAt(10)); err != nil {
		t.Fatalf("pre-activation-time must be a no-op, got %v", err)
	}
}

// TestNonCheckpointIsNoop — activated and at/above activation height, but the
// height is not a checkpoint => nil (certs ride checkpoints only).
func TestNonCheckpointIsNoop(t *testing.T) {
	g := newGate(10, NewMemCertStore())
	// height 15 is activated (>=10) but not a checkpoint (15 % 10 != 0).
	if err := g.VerifyAccepted(checkpointAt(15)); err != nil {
		t.Fatalf("non-checkpoint must be a no-op, got %v", err)
	}
	if !g.Activated(15) {
		t.Fatal("height 15 should be activated")
	}
	if g.IsCheckpoint(15) {
		t.Fatal("height 15 must not be a checkpoint")
	}
}

// TestMissingCertFailsClosed — activated checkpoint with no cert in the store =>
// ErrFinalityCertMissing. Post-activation a checkpoint without PQ evidence must
// NOT finalize.
func TestMissingCertFailsClosed(t *testing.T) {
	g := newGate(10, NewMemCertStore())
	err := g.VerifyAccepted(checkpointAt(20))
	if !errors.Is(err, ErrFinalityCertMissing) {
		t.Fatalf("want ErrFinalityCertMissing, got %v", err)
	}
}

// TestMismatchedCertRejected — a cert that does not bind the finalized block
// (wrong block id / height / chain) is rejected by bindCheck before any crypto.
// Anti-replay: a valid cert for a different block must not satisfy this one.
func TestMismatchedCertRejected(t *testing.T) {
	cases := []struct {
		name string
		cert *qcert.ConsensusCert
	}{
		{"wrong block", &qcert.ConsensusCert{ChainID: 1337, Height: 20, BlockHash: [32]byte{0x99}}},
		{"wrong height", &qcert.ConsensusCert{ChainID: 1337, Height: 21, BlockHash: [32]byte{0xab, 0xcd, 0xef}}},
		{"wrong chain", &qcert.ConsensusCert{ChainID: 7, Height: 20, BlockHash: [32]byte{0xab, 0xcd, 0xef}}},
		{"wrong state root", &qcert.ConsensusCert{ChainID: 1337, Height: 20, BlockHash: [32]byte{0xab, 0xcd, 0xef}, StateRoot: [32]byte{0x55}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemCertStore()
			// Index it at the checkpoint's lookup key so Lookup returns it and
			// bindCheck (not Lookup) does the rejecting.
			store.certs[certKey{chainID: 1337, height: 20, blockID: [32]byte{0xab, 0xcd, 0xef}}] = tc.cert
			g := newGate(10, store)
			err := g.VerifyAccepted(checkpointAt(20))
			if !errors.Is(err, ErrFinalityCertMismatch) {
				t.Fatalf("want ErrFinalityCertMismatch, got %v", err)
			}
		})
	}
}

// TestDelegatesToVerifier — a cert that BINDS correctly and passes the full
// ConsensusCert header path (version, policy load, required-legs root, validator
// -set root) but carries no signature evidence is rejected by the REAL
// consensus verifier, and the gate surfaces it as ErrFinalityCertInvalid. This
// proves the whole delegation chain is wired: policyStore + ValidatorSet +
// quasar.VerifyConsensusCert are reached with matching commitments — everything
// up to (but not including) the leg signature crypto, which needs the producer.
func TestDelegatesToVerifier(t *testing.T) {
	cp := checkpointAt(20)

	// Mirror the gate's posture to compute the header commitments the verifier
	// pins (policy id + required-legs root). policyID and required legs derive
	// from the mode + ML-DSA param, which match the gate's config.
	pol := qcert.NewQuasarEvidencePolicy(DefaultMode, 0, 100)
	cert := &qcert.ConsensusCert{
		Version:          1,
		ChainID:          1337, // must equal the gate's configured ChainID
		Epoch:            cp.Epoch,
		Height:           cp.Height,
		BlockHash:        cp.BlockID,
		PolicyID:         pol.EvidencePolicyID(),
		RequiredLegsRoot: qcert.HashRequiredLegs(pol.RequiredLegs()),
		ValidatorSetRoot: vsRoot,
		// Evidence intentionally empty: the verifier must reject a required leg
		// with no evidence (deepest deterministic failure without real crypto).
	}
	store := NewMemCertStore()
	store.Put(cert)
	g := newGate(10, store)

	err := g.VerifyAccepted(cp)
	if !errors.Is(err, ErrFinalityCertInvalid) {
		t.Fatalf("want ErrFinalityCertInvalid (delegated), got %v", err)
	}
}

// TestValidatorSetUnavailable — a bound cert at an activated checkpoint, but the
// provider has no set for the epoch => ErrValidatorSetUnavailable (fail closed).
func TestValidatorSetUnavailable(t *testing.T) {
	cp := checkpointAt(20)
	cert := &qcert.ConsensusCert{Version: 1, ChainID: 1337, Height: cp.Height, BlockHash: cp.BlockID}
	store := NewMemCertStore()
	store.Put(cert)
	g := NewGate(Config{
		ChainID:            1337,
		Activation:         ActivationConfig{Height: 10},
		CheckpointInterval: 10,
	}, store, StaticValidatorSetProvider{Set: nil}) // no set
	err := g.VerifyAccepted(cp)
	if !errors.Is(err, ErrValidatorSetUnavailable) {
		t.Fatalf("want ErrValidatorSetUnavailable, got %v", err)
	}
}

// --- producer scaffolding ---

type stubProducer struct {
	cert *qcert.ConsensusCert
	hits int
}

func (s *stubProducer) Produce(_ context.Context, _ Subject) (*qcert.ConsensusCert, error) {
	s.hits++
	return s.cert, nil
}

// TestMaybeProduceVerifyOnlyByDefault — a nil producer is the verify-only
// default: MaybeProduce short-circuits to (nil, nil), never panics.
func TestMaybeProduceVerifyOnlyByDefault(t *testing.T) {
	g := newGate(10, NewMemCertStore())
	cert, err := g.MaybeProduce(context.Background(), nil, checkpointAt(20))
	if err != nil || cert != nil {
		t.Fatalf("nil producer must yield (nil,nil), got cert=%v err=%v", cert, err)
	}
}

// TestMaybeProduceDormant — even with a producer wired, a dormant gate produces
// nothing (the producer is brought up before activation is forward-dated).
func TestMaybeProduceDormant(t *testing.T) {
	store := NewMemCertStore()
	g := NewGate(Config{CheckpointInterval: 10}, store, StaticValidatorSetProvider{Set: testValidators()})
	p := &stubProducer{cert: &qcert.ConsensusCert{}}
	cert, err := g.MaybeProduce(context.Background(), p, checkpointAt(20))
	if err != nil || cert != nil {
		t.Fatalf("dormant gate must not produce, got cert=%v err=%v", cert, err)
	}
	if p.hits != 0 {
		t.Fatalf("producer must not be called while dormant, hits=%d", p.hits)
	}
}

// TestMaybeProduceActiveCheckpoint — wired producer + activated checkpoint =>
// the producer is asked for the cert.
func TestMaybeProduceActiveCheckpoint(t *testing.T) {
	g := newGate(10, NewMemCertStore())
	want := &qcert.ConsensusCert{ChainID: 1337, Height: 20}
	p := &stubProducer{cert: want}
	got, err := g.MaybeProduce(context.Background(), p, checkpointAt(20))
	if err != nil {
		t.Fatalf("unexpected err %v", err)
	}
	if got != want || p.hits != 1 {
		t.Fatalf("producer not invoked as expected: got=%v hits=%d", got, p.hits)
	}
	// non-checkpoint height must not invoke the producer
	p2 := &stubProducer{cert: want}
	if _, _ = g.MaybeProduce(context.Background(), p2, checkpointAt(15)); p2.hits != 0 {
		t.Fatalf("producer invoked at non-checkpoint, hits=%d", p2.hits)
	}
}
