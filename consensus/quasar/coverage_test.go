// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// --- Config edge cases ---

func TestDefaultThresholdParamsSmall(t *testing.T) {
	// numParties=1: threshold clamped to numParties
	p := DefaultThresholdParams(1)
	if p.Threshold != 1 {
		t.Errorf("expected threshold 1 for 1 party, got %d", p.Threshold)
	}

	// numParties=2: 2/3*2 + 1 = 2, min is 2
	p = DefaultThresholdParams(2)
	if p.Threshold != 2 {
		t.Errorf("expected threshold 2, got %d", p.Threshold)
	}

	// numParties=3: 2/3*3 + 1 = 3
	p = DefaultThresholdParams(3)
	if p.Threshold != 3 {
		t.Errorf("expected threshold 3, got %d", p.Threshold)
	}
}

func TestQuorumParamsValidate(t *testing.T) {
	p := DefaultQuorumParams()
	if err := p.Validate(); err != nil {
		t.Errorf("default quorum should be valid: %v", err)
	}

	p = QuorumParams{Numerator: 1, Denominator: 0}
	if err := p.Validate(); err == nil {
		t.Error("zero denominator should be invalid")
	}

	p = QuorumParams{Numerator: 4, Denominator: 3}
	if err := p.Validate(); err == nil {
		t.Error("num > denom should be invalid")
	}
}

func TestQuorumParamsIsMet(t *testing.T) {
	p := QuorumParams{Numerator: 2, Denominator: 3}
	if !p.IsMet(200, 300) {
		t.Error("200/300 should meet 2/3 quorum")
	}
	if p.IsMet(199, 300) {
		t.Error("199/300 should not meet 2/3 quorum")
	}
}

func TestQuorumParamsRequiredWeight(t *testing.T) {
	p := QuorumParams{Numerator: 2, Denominator: 3}
	if p.RequiredWeight(300) != 200 {
		t.Errorf("expected 200, got %d", p.RequiredWeight(300))
	}
}

func TestConfigBuilderWithFinalityChannelSize(t *testing.T) {
	cfg, err := NewConfigBuilder().
		WithThreshold(3).
		WithFinalityChannelSize(256).
		Build()
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if cfg.Runtime.FinalityChannelSize != 256 {
		t.Errorf("expected 256, got %d", cfg.Runtime.FinalityChannelSize)
	}
}

func TestThresholdParamsValidateEdge(t *testing.T) {
	p := ThresholdParams{NumParties: 3, Threshold: 5}
	if err := p.Validate(); err == nil {
		t.Error("threshold > parties should be invalid")
	}

	p = ThresholdParams{NumParties: 3, Threshold: 0}
	if err := p.Validate(); err == nil {
		t.Error("threshold 0 should be invalid")
	}
}

// --- Quasar lifecycle ---

func TestQuasarGetCoreGetRingtail(t *testing.T) {
	q, err := NewQuasar(log.Noop(), 2, 2, 3)
	if err != nil {
		t.Fatal(err)
	}

	if q.GetCore() == nil {
		t.Error("GetCore should not return nil")
	}
	if q.GetRingtail() != nil {
		t.Error("Corona should be nil before initialization")
	}
}

func TestQuasarSetGetFinalized(t *testing.T) {
	q, err := NewQuasar(log.Noop(), 2, 2, 3)
	if err != nil {
		t.Fatal(err)
	}

	blockID := ids.GenerateTestID()
	finality := &QuantumFinality{
		BlockID:      blockID,
		PChainHeight: 42,
		Timestamp:    time.Now(),
	}

	q.SetFinalized(blockID, finality)

	got, ok := q.GetFinalized(blockID)
	if !ok {
		t.Fatal("should find finality record")
	}
	if got.PChainHeight != 42 {
		t.Errorf("height mismatch: %d", got.PChainHeight)
	}

	_, ok = q.GetFinalized(ids.GenerateTestID())
	if ok {
		t.Error("should not find non-existent finality")
	}
}

func TestQuasarGetConfig(t *testing.T) {
	q, err := NewQuasar(log.Noop(), 3, 2, 3)
	if err != nil {
		t.Fatal(err)
	}

	threshold, qNum, qDen := q.GetConfig()
	if threshold != 3 {
		t.Errorf("expected threshold 3, got %d", threshold)
	}
	if qNum != 2 || qDen != 3 {
		t.Errorf("expected quorum 2/3, got %d/%d", qNum, qDen)
	}
}

func TestQuasarIsRunning(t *testing.T) {
	q, err := NewQuasar(log.Noop(), 2, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if q.IsRunning() {
		t.Error("should not be running before Start")
	}
}

func TestQuasarCheckQuorum(t *testing.T) {
	q, err := NewQuasar(log.Noop(), 2, 2, 3)
	if err != nil {
		t.Fatal(err)
	}

	if !q.CheckQuorum(200, 300) {
		t.Error("200/300 should meet 2/3 quorum")
	}
	if q.CheckQuorum(100, 300) {
		t.Error("100/300 should not meet 2/3 quorum")
	}
}

func TestQuasarCreateMessage(t *testing.T) {
	q, err := NewQuasar(log.Noop(), 2, 2, 3)
	if err != nil {
		t.Fatal(err)
	}

	event := FinalityEvent{
		BlockID: ids.GenerateTestID(),
		Height:  100,
	}

	msg := q.CreateMessage(event)
	if len(msg) == 0 {
		t.Error("message should not be empty")
	}
}

func TestQuasarTotalWeight(t *testing.T) {
	q, err := NewQuasar(log.Noop(), 2, 2, 3)
	if err != nil {
		t.Fatal(err)
	}

	validators := []ValidatorState{
		{Weight: 100, Active: true},
		{Weight: 200, Active: true},
		{Weight: 50, Active: true},
	}

	total := q.TotalWeight(validators)
	if total != 350 {
		t.Errorf("expected 350, got %d", total)
	}

	// Inactive validators should not count
	validators[2].Active = false
	total = q.TotalWeight(validators)
	if total != 300 {
		t.Errorf("expected 300 without inactive, got %d", total)
	}
}

// --- BLS Signature ---

func TestBLSSignature(t *testing.T) {
	signers := []ids.NodeID{ids.GenerateTestNodeID(), ids.GenerateTestNodeID()}
	sig := NewBLSSignature([]byte("aggregated-sig"), signers)

	if sig.Type() != SignatureTypeBLS {
		t.Error("wrong type")
	}
	if len(sig.Bytes()) == 0 {
		t.Error("bytes should not be empty")
	}
	if len(sig.Signers()) != 2 {
		t.Errorf("expected 2 signers, got %d", len(sig.Signers()))
	}
}

// --- RingtailCoordinator ---

func TestRingtailCoordinatorSignNotInitialized(t *testing.T) {
	rc, _ := NewRingtailCoordinator(log.Noop(), RingtailConfig{NumParties: 3, Threshold: 2})
	_, err := rc.Sign([]byte("msg"))
	if err == nil {
		t.Error("should fail when not initialized")
	}
}

func TestRingtailCoordinatorVerifyNotInitialized(t *testing.T) {
	rc, _ := NewRingtailCoordinator(log.Noop(), RingtailConfig{NumParties: 3, Threshold: 2})
	if rc.Verify([]byte("msg"), nil) {
		t.Error("should return false when not initialized")
	}
}

func TestRingtailCoordinatorTestMode(t *testing.T) {
	rc, _ := NewTestRingtailCoordinator(log.Noop(), RingtailConfig{NumParties: 3, Threshold: 2})
	validators := []ids.NodeID{ids.GenerateTestNodeID()}
	rc.Initialize(validators)

	sig, err := rc.Sign([]byte("test-message"))
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	if sig == nil {
		t.Fatal("signature should not be nil")
	}
	if !rc.Verify([]byte("test-message"), sig) {
		t.Error("should verify in testing mode")
	}
}

func TestRingtailCoordinatorNotTestMode(t *testing.T) {
	rc, _ := NewRingtailCoordinator(log.Noop(), RingtailConfig{NumParties: 3, Threshold: 2})
	rc.Initialize([]ids.NodeID{ids.GenerateTestNodeID()})

	_, err := rc.Sign([]byte("msg"))
	if err == nil {
		t.Error("should fail when not in testing mode")
	}

	sig := NewRingtailSignature([]byte("fake"), nil)
	if rc.Verify([]byte("msg"), sig) {
		t.Error("should return false when not in testing mode")
	}
}

func TestRingtailCoordinatorStats(t *testing.T) {
	rc, _ := NewRingtailCoordinator(log.Noop(), RingtailConfig{NumParties: 3, Threshold: 2})
	s := rc.Stats()
	if s.NumParties != 3 {
		t.Errorf("expected 3 parties, got %d", s.NumParties)
	}
	if s.Initialized {
		t.Error("should not be initialized")
	}
}

func TestRingtailCoordinatorThresholdNumParties(t *testing.T) {
	rc, _ := NewRingtailCoordinator(log.Noop(), RingtailConfig{NumParties: 5, Threshold: 3})
	if rc.Threshold() != 3 {
		t.Errorf("expected threshold 3, got %d", rc.Threshold())
	}
	if rc.NumParties() != 5 {
		t.Errorf("expected 5 parties, got %d", rc.NumParties())
	}
}
