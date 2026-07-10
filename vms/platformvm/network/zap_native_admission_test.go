// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/proto/zap_native"
)

// LP-023 Red round 7 R7V5 — admission-gate tests.
//
// Threat model: an adversary submits a tx whose kind has no executor
// body yet. Without the gate, the executor would return
// "not yet implemented" mid-block — by which time the tx has already
// been gossiped, sat in the mempool, and consumed RAM. The gate
// REFUSES at admission so the tx never enters the mempool at all.

// errSentinel is the inner-verifier sentinel used to confirm the gate
// delegates to the wrapped verifier when admission is allowed.
var errSentinel = errors.New("inner verifier hit")

// passThroughVerifier returns errSentinel for every tx — used to assert
// that the gate ALWAYS calls into the inner verifier for non-gated kinds.
type passThroughVerifier struct{}

func (passThroughVerifier) VerifyTx(*txs.Tx) error { return errSentinel }

// blockEverythingVerifier returns errSentinel for every tx — used as a
// canary to confirm gated kinds never reach the inner verifier.
type blockEverythingVerifier struct{}

func (blockEverythingVerifier) VerifyTx(*txs.Tx) error { return errSentinel }

// TestZapNativeAdmissionGate_RejectsCreateSovereignL1Tx pins R7V5: the
// gate must refuse a legacy *txs.CreateSovereignL1Tx with
// ErrZapNativeNotYetExecutable, BEFORE the inner verifier sees the tx.
// The inner verifier here returns errSentinel for everything — if it
// fires we'd see errSentinel, not ErrZapNativeNotYetExecutable.
func TestZapNativeAdmissionGate_RejectsCreateSovereignL1Tx(t *testing.T) {
	gate := NewZapNativeAdmissionGate(blockEverythingVerifier{})
	tx := &txs.Tx{Unsigned: &txs.CreateSovereignL1Tx{}}
	err := gate.VerifyTx(tx)
	if !errors.Is(err, ErrZapNativeNotYetExecutable) {
		t.Fatalf(
			"gate.VerifyTx(CreateSovereignL1Tx) = %v, want ErrZapNativeNotYetExecutable",
			err,
		)
	}
	if errors.Is(err, errSentinel) {
		t.Fatalf("gate let CreateSovereignL1Tx reach the inner verifier: %v", err)
	}
}

// TestZapNativeAdmissionGate_PassThroughLegacyTypes pins the brief:
// "Legacy txs.ConvertNetworkToL1Tx / txs.RegisterL1ValidatorTx (the working
// ones) still pass through". For txs.BaseTx (working executor) and
// txs.RegisterL1ValidatorTx + txs.ConvertNetworkToL1Tx (legacy
// executors at line 639/761), the gate must NOT fire; the inner
// verifier MUST see the tx (we assert via errSentinel).
func TestZapNativeAdmissionGate_PassThroughLegacyTypes(t *testing.T) {
	gate := NewZapNativeAdmissionGate(passThroughVerifier{})

	cases := []struct {
		name string
		tx   *txs.Tx
	}{
		{"BaseTx", &txs.Tx{Unsigned: &txs.BaseTx{}}},
		{"RegisterL1ValidatorTx", &txs.Tx{Unsigned: &txs.RegisterL1ValidatorTx{}}},
		{"ConvertNetworkToL1Tx", &txs.Tx{Unsigned: &txs.ConvertNetworkToL1Tx{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := gate.VerifyTx(tc.tx)
			if !errors.Is(err, errSentinel) {
				t.Fatalf(
					"gate.VerifyTx(%s) = %v, want errSentinel (gate must pass to inner)",
					tc.name, err,
				)
			}
			if errors.Is(err, ErrZapNativeNotYetExecutable) {
				t.Fatalf(
					"gate FALSE-POSITIVE on %s: returned ErrZapNativeNotYetExecutable for a kind with a live executor",
					tc.name,
				)
			}
		})
	}
}

// TestZapNativeAdmissionGate_RejectsZapWireCreateSovereignL1 exercises
// the future-proof half: a wire buffer that PARSES as zap_native.
// CreateSovereignL1Tx (TxKind=23) must be refused even if the legacy
// Unsigned struct field is something innocuous. The gate inspects the
// signed bytes via zap_native.IsZAPBytes + Wrap*Tx; the kind discriminator
// is the truth source.
func TestZapNativeAdmissionGate_RejectsZapWireCreateSovereignL1(t *testing.T) {
	stub := zap_native.OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x42}}
	zapTx := zap_native.NewCreateSovereignL1Tx(zap_native.CreateSovereignL1TxInput{
		NetworkID:  1,
		Owner:      stub,
		Validators: []zap_native.ValidatorsListEntry{newWireFriendlyValidator(t)},
		Chains: []zap_native.ChainsListEntry{
			{Name: []byte("evm"), VMID: ids.ID{0xed, 0xed}, FxIDs: []ids.ID{{0x01}}, GenesisData: []byte("g")},
		},
	})
	wireBytes := zapTx.Bytes()
	if !zap_native.IsZAPBytes(wireBytes) {
		t.Fatalf("baseline: expected wire bytes to be ZAP-magic, got prefix=%q", string(wireBytes[:4]))
	}

	tx := &txs.Tx{Unsigned: &txs.BaseTx{}} // innocuous legacy struct
	tx.SetBytes(wireBytes, wireBytes)

	gate := NewZapNativeAdmissionGate(blockEverythingVerifier{})
	err := gate.VerifyTx(tx)
	if !errors.Is(err, ErrZapNativeNotYetExecutable) {
		t.Fatalf(
			"gate.VerifyTx(zap-wire CreateSovereignL1) = %v, want ErrZapNativeNotYetExecutable",
			err,
		)
	}
}

// TestZapNativeAdmissionGate_RejectsZapWireRegisterL1Validator exercises
// the wire-kind=7 (RegisterL1Validator) gate path. Even though the
// legacy struct executor is live, the zap_native wire path is still
// gated until the wire-aware admission flow lands.
func TestZapNativeAdmissionGate_RejectsZapWireRegisterL1Validator(t *testing.T) {
	zapTx := zap_native.NewRegisterL1ValidatorTx(
		ids.ID{0xAA},
		[zap_native.BLSPubKeySize]byte{},
		[zap_native.BLSPoPSize]byte{},
		1_900_000_000,
		ids.ID{0xBB},
	)
	wireBytes := zapTx.Bytes()
	if !zap_native.IsZAPBytes(wireBytes) {
		t.Fatalf("baseline: expected wire bytes to be ZAP-magic, got prefix=%q", string(wireBytes[:4]))
	}

	tx := &txs.Tx{Unsigned: &txs.BaseTx{}}
	tx.SetBytes(wireBytes, wireBytes)

	gate := NewZapNativeAdmissionGate(blockEverythingVerifier{})
	err := gate.VerifyTx(tx)
	if !errors.Is(err, ErrZapNativeNotYetExecutable) {
		t.Fatalf(
			"gate.VerifyTx(zap-wire RegisterL1Validator) = %v, want ErrZapNativeNotYetExecutable",
			err,
		)
	}
}

// TestZapNativeAdmissionGate_RejectsZapWireConvertNetworkToL1 exercises
// the wire-kind=22 (ConvertNetworkToL1) gate path.
func TestZapNativeAdmissionGate_RejectsZapWireConvertNetworkToL1(t *testing.T) {
	zapTx := zap_native.NewConvertNetworkToL1Tx(zap_native.ConvertNetworkToL1TxInput{
		NetworkID:      1,
		BlockchainID:   ids.ID{0x11},
		Chain:          ids.ID{0x22},
		ManagerChainID: ids.ID{0x33},
		Address:        []byte("0x" + "00"),
		Validators:     []zap_native.ValidatorsListEntry{newWireFriendlyValidator(t)},
	})
	wireBytes := zapTx.Bytes()
	if !zap_native.IsZAPBytes(wireBytes) {
		t.Fatalf("baseline: expected wire bytes to be ZAP-magic, got prefix=%q", string(wireBytes[:4]))
	}

	tx := &txs.Tx{Unsigned: &txs.BaseTx{}}
	tx.SetBytes(wireBytes, wireBytes)

	gate := NewZapNativeAdmissionGate(blockEverythingVerifier{})
	err := gate.VerifyTx(tx)
	if !errors.Is(err, ErrZapNativeNotYetExecutable) {
		t.Fatalf(
			"gate.VerifyTx(zap-wire ConvertNetworkToL1) = %v, want ErrZapNativeNotYetExecutable",
			err,
		)
	}
}

// TestZapNativeAdmissionGate_PassThroughNonZapBytes pins the gate's
// non-interference contract: a tx whose Bytes() are NOT ZAP-magic
// (e.g. legacy reflection codec-encoded) and whose Unsigned is a working
// executor type MUST be delegated to the inner verifier unmodified.
func TestZapNativeAdmissionGate_PassThroughNonZapBytes(t *testing.T) {
	tx := &txs.Tx{Unsigned: &txs.BaseTx{}}
	// Non-ZAP wire bytes — anything that doesn't start with "ZAP\x00".
	tx.SetBytes([]byte{0x00, 0x00, 0x00, 0x01, 0xDE, 0xAD, 0xBE, 0xEF}, []byte{0x00, 0x00, 0x00, 0x01, 0xDE, 0xAD, 0xBE, 0xEF})

	gate := NewZapNativeAdmissionGate(passThroughVerifier{})
	err := gate.VerifyTx(tx)
	if !errors.Is(err, errSentinel) {
		t.Fatalf("gate.VerifyTx(non-ZAP) = %v, want errSentinel (gate must pass to inner)", err)
	}
}

// TestZapNativeAdmissionGate_NilSafety confirms the gate handles
// edge-case inputs (nil tx, nil Unsigned, empty Bytes) without
// panicking. Defense-in-depth: the gate is on the hot path, so
// it MUST be panic-free on every input.
func TestZapNativeAdmissionGate_NilSafety(t *testing.T) {
	gate := NewZapNativeAdmissionGate(passThroughVerifier{})

	t.Run("nil tx", func(t *testing.T) {
		// Inner verifier (passThroughVerifier) returns errSentinel on
		// any input including nil; the gate must NOT panic on the way.
		err := gate.VerifyTx(nil)
		if !errors.Is(err, errSentinel) {
			t.Fatalf("gate.VerifyTx(nil) = %v, want errSentinel", err)
		}
	})
	t.Run("nil Unsigned", func(t *testing.T) {
		err := gate.VerifyTx(&txs.Tx{Unsigned: nil})
		if !errors.Is(err, errSentinel) {
			t.Fatalf("gate.VerifyTx(nilUnsigned) = %v, want errSentinel", err)
		}
	})
	t.Run("empty bytes", func(t *testing.T) {
		tx := &txs.Tx{Unsigned: &txs.BaseTx{}}
		tx.SetBytes(nil, nil)
		err := gate.VerifyTx(tx)
		if !errors.Is(err, errSentinel) {
			t.Fatalf("gate.VerifyTx(emptyBytes) = %v, want errSentinel", err)
		}
	})
}

// newWireFriendlyValidator returns a ValidatorsListEntry whose
// constructor-time BLS fields are present (even if zeroed). The R7V5
// admission gate does NOT verify BLS PoP — that's R7V7 / Verify()'s
// job — so this minimal entry is sufficient for the gate test.
func newWireFriendlyValidator(t *testing.T) zap_native.ValidatorsListEntry {
	t.Helper()
	return zap_native.ValidatorsListEntry{
		NodeID:             ids.NodeID{0x77},
		Weight:             1_000_000,
		BLSPubKey:          [zap_native.BLSPubKeySize]byte{},
		BLSPoP:             [zap_native.BLSPoPSize]byte{},
		RegistrationExpiry: 1_900_000_000,
	}
}
