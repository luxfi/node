// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"errors"
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// TestVerify_AcceptsWellFormed walks every tx type that embeds an
// OwnerStub and pins that Verify() returns nil on a well-formed tx.
// Threshold=1, single address — the legitimate single-signer case.
func TestVerify_AcceptsWellFormed(t *testing.T) {
	addr := ids.ShortID{0x42}
	stub := OwnerStub{Threshold: 1, Locktime: 0, Address: addr}

	t.Run("AddValidator", func(t *testing.T) {
		tx := NewAddValidatorTx(AddValidatorTxInput{NetworkID: 1, RewardsOwner: stub})
		if err := tx.Verify(); err != nil {
			t.Fatalf("Verify = %v, want nil", err)
		}
	})
	t.Run("AddDelegator", func(t *testing.T) {
		tx := NewAddDelegatorTx(AddDelegatorTxInput{NetworkID: 1, DelegationRewardsOwner: stub})
		if err := tx.Verify(); err != nil {
			t.Fatalf("Verify = %v, want nil", err)
		}
	})
	t.Run("AddPermissionlessValidator", func(t *testing.T) {
		tx := NewAddPermissionlessValidatorTx(AddPermissionlessValidatorTxInput{
			NetworkID:              1,
			ValidationRewardsOwner: stub,
			DelegationRewardsOwner: stub,
		})
		if err := tx.Verify(); err != nil {
			t.Fatalf("Verify = %v, want nil", err)
		}
	})
	t.Run("AddPermissionlessDelegator", func(t *testing.T) {
		tx := NewAddPermissionlessDelegatorTx(AddPermissionlessDelegatorTxInput{
			NetworkID:              1,
			DelegationRewardsOwner: stub,
		})
		if err := tx.Verify(); err != nil {
			t.Fatalf("Verify = %v, want nil", err)
		}
	})
	t.Run("CreateChain", func(t *testing.T) {
		tx := NewCreateChainTx(CreateChainTxInput{NetworkID: 1, Owner: stub})
		if err := tx.Verify(); err != nil {
			t.Fatalf("Verify = %v, want nil", err)
		}
	})
	t.Run("CreateNetwork", func(t *testing.T) {
		tx := NewCreateNetworkTx(CreateNetworkTxInput{NetworkID: 1, Owner: stub})
		if err := tx.Verify(); err != nil {
			t.Fatalf("Verify = %v, want nil", err)
		}
	})
	t.Run("CreateSovereignL1", func(t *testing.T) {
		tx := NewCreateSovereignL1Tx(CreateSovereignL1TxInput{NetworkID: 1, Owner: stub})
		if err := tx.Verify(); err != nil {
			t.Fatalf("Verify = %v, want nil", err)
		}
	})
}

// TestVerify_RejectsMaliciousThresholdZero pins that the executor-side
// Verify() catches a wire-encoded tx whose embedded OwnerStub has
// threshold == 0. This is the highest-severity R4V7 attack: a malicious
// encoder publishes a stub with no quorum, and the consumer treats it
// as authorized.
//
// We hand-craft the wire shape: build a real tx with threshold=1, then
// re-parse and overwrite the threshold byte to 0 via the builder.
// (Simpler: we go through NewXxxTx with a hand-supplied OwnerStub that
// has threshold=0 — bypassing the constructor isn't needed for this
// test because zap_native's NewXxxTx functions don't pre-validate.)
func TestVerify_RejectsMaliciousThresholdZero(t *testing.T) {
	addr := ids.ShortID{0x42}
	malicious := OwnerStub{Threshold: 0, Locktime: 0, Address: addr}

	t.Run("AddValidator", func(t *testing.T) {
		tx := NewAddValidatorTx(AddValidatorTxInput{NetworkID: 1, RewardsOwner: malicious})
		err := tx.Verify()
		if !errors.Is(err, ErrOwnerThresholdZero) {
			t.Fatalf("Verify = %v, want ErrOwnerThresholdZero", err)
		}
	})
	t.Run("AddDelegator", func(t *testing.T) {
		tx := NewAddDelegatorTx(AddDelegatorTxInput{NetworkID: 1, DelegationRewardsOwner: malicious})
		if !errors.Is(tx.Verify(), ErrOwnerThresholdZero) {
			t.Fatalf("missing reject")
		}
	})
	t.Run("APV_ValidationOwner", func(t *testing.T) {
		good := OwnerStub{Threshold: 1, Address: addr}
		tx := NewAddPermissionlessValidatorTx(AddPermissionlessValidatorTxInput{
			NetworkID:              1,
			ValidationRewardsOwner: malicious,
			DelegationRewardsOwner: good,
		})
		if !errors.Is(tx.Verify(), ErrOwnerThresholdZero) {
			t.Fatalf("missing reject on ValidationOwner=malicious")
		}
	})
	t.Run("APV_DelegationOwner", func(t *testing.T) {
		good := OwnerStub{Threshold: 1, Address: addr}
		tx := NewAddPermissionlessValidatorTx(AddPermissionlessValidatorTxInput{
			NetworkID:              1,
			ValidationRewardsOwner: good,
			DelegationRewardsOwner: malicious,
		})
		if !errors.Is(tx.Verify(), ErrOwnerThresholdZero) {
			t.Fatalf("missing reject on DelegationOwner=malicious")
		}
	})
	t.Run("CreateChain", func(t *testing.T) {
		tx := NewCreateChainTx(CreateChainTxInput{NetworkID: 1, Owner: malicious})
		if !errors.Is(tx.Verify(), ErrOwnerThresholdZero) {
			t.Fatalf("missing reject")
		}
	})
	t.Run("CreateNetwork", func(t *testing.T) {
		tx := NewCreateNetworkTx(CreateNetworkTxInput{NetworkID: 1, Owner: malicious})
		if !errors.Is(tx.Verify(), ErrOwnerThresholdZero) {
			t.Fatalf("missing reject")
		}
	})
	t.Run("CreateSovereignL1", func(t *testing.T) {
		tx := NewCreateSovereignL1Tx(CreateSovereignL1TxInput{NetworkID: 1, Owner: malicious})
		if !errors.Is(tx.Verify(), ErrOwnerThresholdZero) {
			t.Fatalf("missing reject")
		}
	})
}

// TestVerify_RejectsMaliciousThresholdAboveOne pins the OwnerStub specific
// case where threshold > 1 (unsatisfiable; only one address is present).
func TestVerify_RejectsMaliciousThresholdAboveOne(t *testing.T) {
	bad := OwnerStub{Threshold: 5, Locktime: 0, Address: ids.ShortID{0x42}}

	tx := NewCreateNetworkTx(CreateNetworkTxInput{NetworkID: 1, Owner: bad})
	if !errors.Is(tx.Verify(), ErrOwnerThresholdExceedsAddrs) {
		t.Fatalf("CreateNetwork.Verify = %v, want ErrOwnerThresholdExceedsAddrs", tx.Verify())
	}
}

// TestVerify_AdversarialWireBuffer pins R4V7 against the strongest threat
// model: an adversary controls the BYTE STREAM, not the constructor input.
// We build a CreateNetworkTx the normal way, then overwrite the threshold
// bytes in the buffer (offset 85) to 0, re-Wrap, and confirm Verify()
// catches it. This proves the gate fires on untrusted wire input, not
// only on constructor input.
func TestVerify_AdversarialWireBuffer(t *testing.T) {
	good := OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x42}}
	tx := NewCreateNetworkTx(CreateNetworkTxInput{NetworkID: 1, Owner: good})
	buf := tx.Bytes()

	// The threshold uint32 lives at OffsetCreateNetworkTx_OwnerThreshold
	// (= 85) in the FIXED SECTION. The fixed section starts at the
	// root-object payload offset within buf. Locate it via re-Parse.
	msg, err := zap.Parse(buf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	root := msg.Root()
	// Confirm pre-overwrite threshold == 1.
	if got := root.Uint32(OffsetCreateNetworkTx_OwnerThreshold); got != 1 {
		t.Fatalf("pre-overwrite threshold = %d, want 1", got)
	}

	// To overwrite, we duplicate the buffer and patch the 4 bytes at
	// the root-object's threshold field. The root object's payload start
	// is computed from the wire header. zap exposes the position via the
	// Object's offset; but tests can simply search for the unique
	// pattern (threshold=1, locktime=0, address[0]=0x42) and patch in
	// place.
	tampered := make([]byte, len(buf))
	copy(tampered, buf)

	// We know threshold lives uint32 little-endian; pattern is {0x01,0x00,0x00,0x00}
	// followed by locktime uint64 {0,0,0,0,0,0,0,0} then 20-byte addr
	// starting with 0x42. Scan once.
	patched := false
	for i := 0; i+32 <= len(tampered); i++ {
		if tampered[i] == 0x01 && tampered[i+1] == 0 && tampered[i+2] == 0 && tampered[i+3] == 0 &&
			tampered[i+4] == 0 && tampered[i+5] == 0 && tampered[i+6] == 0 && tampered[i+7] == 0 &&
			tampered[i+8] == 0 && tampered[i+9] == 0 && tampered[i+10] == 0 && tampered[i+11] == 0 &&
			tampered[i+12] == 0x42 {
			// Overwrite the threshold uint32 with 0.
			tampered[i] = 0
			patched = true
			break
		}
	}
	if !patched {
		t.Fatalf("could not locate threshold field in wire buffer")
	}

	// Re-Wrap the tampered buffer.
	tamperedTx, err := WrapCreateNetworkTx(tampered)
	if err != nil {
		t.Fatalf("WrapCreateNetworkTx(tampered) = %v, want nil (parser is permissive)", err)
	}
	thr, _, _ := tamperedTx.Owner()
	if thr != 0 {
		t.Fatalf("tampered threshold = %d, want 0 (patch did not take effect)", thr)
	}
	// Now the executor-side gate must reject.
	if !errors.Is(tamperedTx.Verify(), ErrOwnerThresholdZero) {
		t.Fatalf("Verify(tampered) = %v, want ErrOwnerThresholdZero", tamperedTx.Verify())
	}
}
