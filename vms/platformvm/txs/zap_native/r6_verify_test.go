// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"crypto/rand"
	"errors"
	"testing"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// LP-023 Red round 6 verify-gate tests.
//
// Adversarial threat model: the attacker controls the wire byte stream
// (not the constructor). Each test below constructs a CreateSovereignL1Tx
// (or TransferChainOwnershipTx) the legitimate way, then either:
//   - omits a required field (zero validators / zero chains) via the
//     constructor, OR
//   - patches a field in the wire buffer post-encode to a malicious
//     value and re-Wraps.
// The executor-side Verify() gate MUST catch it.

// TestCreateSovereignL1Tx_Verify_RejectsZeroValidators pins R6V4 critical:
// a zero-validator L1 cannot bootstrap consensus. Verify must reject at
// the wire boundary.
func TestCreateSovereignL1Tx_Verify_RejectsZeroValidators(t *testing.T) {
	stub := OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x42}}
	tx := NewCreateSovereignL1Tx(CreateSovereignL1TxInput{
		NetworkID:  1,
		Owner:      stub,
		Validators: nil, // adversarial empty set
		Chains:     []ChainsListEntry{makeValidChainsEntry()},
	})
	err := tx.Verify()
	if !errors.Is(err, ErrZeroValidators) {
		t.Fatalf("Verify(zero validators) = %v, want ErrZeroValidators", err)
	}
}

// TestCreateSovereignL1Tx_Verify_RejectsZeroChains pins R6V4 critical:
// a zero-chain L1 is malformed; Verify must reject.
func TestCreateSovereignL1Tx_Verify_RejectsZeroChains(t *testing.T) {
	stub := OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x42}}
	tx := NewCreateSovereignL1Tx(CreateSovereignL1TxInput{
		NetworkID:  1,
		Owner:      stub,
		Validators: []ValidatorsListEntry{makeValidVerifyingValidator(t)},
		Chains:     nil, // adversarial empty list
	})
	err := tx.Verify()
	if !errors.Is(err, ErrZeroChains) {
		t.Fatalf("Verify(zero chains) = %v, want ErrZeroChains", err)
	}
}

// TestCreateSovereignL1Tx_Verify_RejectsZeroWeight pins R6V4: a
// zero-weight validator skews quorum (it pads the validator count but
// contributes nothing to threshold), so reject. Construct two validators
// where the second has weight=0.
func TestCreateSovereignL1Tx_Verify_RejectsZeroWeight(t *testing.T) {
	stub := OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x42}}
	good := makeValidVerifyingValidator(t)
	bad := makeValidVerifyingValidator(t)
	bad.Weight = 0 // adversarial
	tx := NewCreateSovereignL1Tx(CreateSovereignL1TxInput{
		NetworkID:  1,
		Owner:      stub,
		Validators: []ValidatorsListEntry{good, bad},
		Chains:     []ChainsListEntry{makeValidChainsEntry()},
	})
	err := tx.Verify()
	if !errors.Is(err, ErrValidatorWeightZero) {
		t.Fatalf("Verify(zero-weight validator) = %v, want ErrValidatorWeightZero", err)
	}
}

// TestCreateSovereignL1Tx_Verify_RejectsBadBLSPoP pins R6V3: a validator
// whose BLS PoP does not verify against the embedded BLS pubkey must be
// rejected. Construct a valid validator, then scramble the PoP bytes
// before encoding.
func TestCreateSovereignL1Tx_Verify_RejectsBadBLSPoP(t *testing.T) {
	stub := OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x42}}
	good := makeValidVerifyingValidator(t)
	bad := good
	// Scramble the PoP into uniform random bytes — pairing fails.
	if _, err := rand.Read(bad.BLSPoP[:]); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	tx := NewCreateSovereignL1Tx(CreateSovereignL1TxInput{
		NetworkID:  1,
		Owner:      stub,
		Validators: []ValidatorsListEntry{bad},
		Chains:     []ChainsListEntry{makeValidChainsEntry()},
	})
	err := tx.Verify()
	if !errors.Is(err, ErrBadBLSPoP) {
		t.Fatalf("Verify(bad PoP) = %v, want ErrBadBLSPoP", err)
	}
}

// TestCreateSovereignL1Tx_Verify_RejectsBadBLSPoP_MismatchedPubKey pins
// R6V3 against the keypair-substitution attack: an adversary takes a
// valid PoP from validator A and pairs it with validator B's pubkey,
// hoping the wire-level verify lets it through. The pairing must reject.
func TestCreateSovereignL1Tx_Verify_RejectsBadBLSPoP_MismatchedPubKey(t *testing.T) {
	stub := OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x42}}
	a := makeValidVerifyingValidator(t)
	b := makeValidVerifyingValidator(t)
	// Splice: keep B's pubkey, swap in A's PoP. PoP signs A's pubkey, not B's.
	hybrid := b
	hybrid.BLSPoP = a.BLSPoP
	tx := NewCreateSovereignL1Tx(CreateSovereignL1TxInput{
		NetworkID:  1,
		Owner:      stub,
		Validators: []ValidatorsListEntry{hybrid},
		Chains:     []ChainsListEntry{makeValidChainsEntry()},
	})
	err := tx.Verify()
	if !errors.Is(err, ErrBadBLSPoP) {
		t.Fatalf("Verify(mismatched-keypair PoP) = %v, want ErrBadBLSPoP", err)
	}
}

// TestChainsList_Verify_RejectsBadFxIDsLen pins R6V5: a chain entry whose
// FxIDsLen is not a multiple of FxIDSize must be rejected. We tamper the
// wire buffer post-encode to set FxIDsLen=63 (not a multiple of 32) and
// Verify must catch it.
func TestChainsList_Verify_RejectsBadFxIDsLen(t *testing.T) {
	stub := OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x42}}
	// Build a legitimate tx with one chain whose FxIDs has 2 entries (64B).
	chain := ChainsListEntry{
		Name:        []byte("evm"),
		VMID:        ids.ID{0xed},
		FxIDs:       []ids.ID{{0x01}, {0x02}},
		GenesisData: []byte("g"),
	}
	tx := NewCreateSovereignL1Tx(CreateSovereignL1TxInput{
		NetworkID:  1,
		Owner:      stub,
		Validators: []ValidatorsListEntry{makeValidVerifyingValidator(t)},
		Chains:     []ChainsListEntry{chain},
	})
	// Pre-tamper sanity: Verify must succeed.
	if err := tx.Verify(); err != nil {
		t.Fatalf("baseline Verify = %v, want nil", err)
	}

	// Tamper: locate ChainEntry.FxIDsLen in the wire buffer. The legitimate
	// value is 64 (=2 * FxIDSize). We scan for the unique 4-byte LE pattern
	// {0x40, 0x00, 0x00, 0x00} and overwrite with 63.
	//
	// To make the search unique we also pin neighborhood context: the
	// VMID's first byte (0xed) lives at OffsetChainEntry_VMID (=8) within
	// the entry, and FxIDsLen lives at offset 44. So we search for
	// 0xed followed by 31 trailing VMID bytes, then 4 bytes of FxIDsRel
	// (=0), then the {0x40,0,0,0} length pattern.
	buf := tx.Bytes()
	tampered := make([]byte, len(buf))
	copy(tampered, buf)
	patched := false
	for i := 0; i+SizeChainEntry <= len(tampered); i++ {
		if tampered[i] != 0xed {
			continue
		}
		// Check rest of VMID is zero (we set VMID = {0xed} only).
		zeroVMID := true
		for j := 1; j < 32; j++ {
			if tampered[i+j] != 0 {
				zeroVMID = false
				break
			}
		}
		if !zeroVMID {
			continue
		}
		// FxIDsRel at +32, FxIDsLen at +36. FxIDsRel should be 0 (first entry).
		if tampered[i+32] != 0 || tampered[i+33] != 0 || tampered[i+34] != 0 || tampered[i+35] != 0 {
			continue
		}
		// FxIDsLen at +36 should be 64 (= 2 * 32).
		if tampered[i+36] != 0x40 || tampered[i+37] != 0 || tampered[i+38] != 0 || tampered[i+39] != 0 {
			continue
		}
		// Patch FxIDsLen to 63 (malformed).
		tampered[i+36] = 0x3F
		patched = true
		break
	}
	if !patched {
		t.Fatalf("could not locate ChainEntry.FxIDsLen in wire buffer")
	}
	tamperedTx, err := WrapCreateSovereignL1Tx(tampered)
	if err != nil {
		t.Fatalf("WrapCreateSovereignL1Tx(tampered) = %v, want nil (parser permissive)", err)
	}
	// Confirm the tamper landed in the wire.
	gotEntry := tamperedTx.Chains().At(0)
	if _, length := gotEntry.FxIDsRange(); length != 63 {
		t.Fatalf("tampered FxIDsLen = %d, want 63", length)
	}
	// Verify must reject.
	if err := tamperedTx.Verify(); !errors.Is(err, ErrMalformedFxIDsLen) {
		t.Fatalf("Verify(malformed FxIDsLen) = %v, want ErrMalformedFxIDsLen", err)
	}
}

// TestChainsListView_MustVerify_StandaloneEntries exercises the helper on
// a hand-constructed ChainsListView so MustVerify is callable independently
// of CreateSovereignL1Tx. Defense-in-depth: any future tx that embeds
// ChainsList must call MustVerify() inside its own Verify() body — the
// audit gate (audit_test.go + .github/workflows/zap-audit.yml
// chainslist-verify-gate) enforces this at CI.
//
// LP-023 R7V8: renamed Verify → MustVerify so the consumer-side gate
// is grep-able from the CI workflow.
func TestChainsListView_MustVerify_StandaloneEntries(t *testing.T) {
	// Good entry: FxIDsLen = 2*32 = 64.
	entries := []ChainsListEntry{
		{Name: []byte("a"), VMID: ids.ID{0x01}, FxIDs: []ids.ID{{0xff}, {0xee}}},
		{Name: []byte("b"), VMID: ids.ID{0x02}, FxIDs: nil},
	}
	tx := NewCreateSovereignL1Tx(CreateSovereignL1TxInput{
		NetworkID:  1,
		Owner:      OwnerStub{Threshold: 1, Address: ids.ShortID{0x55}},
		Validators: []ValidatorsListEntry{makeValidVerifyingValidator(t)},
		Chains:     entries,
	})
	if err := tx.Chains().MustVerify(); err != nil {
		t.Fatalf("ChainsListView.MustVerify = %v, want nil", err)
	}
}

// TestTransferChainOwnershipTx_Verify_RejectsZeroThreshold pins R6V8:
// the new Verify() must reject threshold=0. Constructor path — the v3
// tx accepts any (threshold, locktime, address) so we can build a
// malicious-by-construction tx and prove Verify catches it.
func TestTransferChainOwnershipTx_Verify_RejectsZeroThreshold(t *testing.T) {
	addr := ids.ShortID{0x42}
	tx := NewTransferChainOwnershipTx(ids.ID{0xCA}, 0, 0, addr)
	err := tx.Verify()
	if !errors.Is(err, ErrOwnerThresholdZero) {
		t.Fatalf("Verify(threshold=0) = %v, want ErrOwnerThresholdZero", err)
	}
}

// TestTransferChainOwnershipTx_Verify_RejectsThresholdAboveOne pins R6V8:
// the v3 tx pins a single-address Owner, so threshold > 1 is
// unsatisfiable (only one signer, quorum > 1 can never be reached). The
// underlying OwnerStub.SyntacticVerify returns ErrOwnerThresholdExceedsAddrs.
func TestTransferChainOwnershipTx_Verify_RejectsThresholdAboveOne(t *testing.T) {
	addr := ids.ShortID{0x42}
	tx := NewTransferChainOwnershipTx(ids.ID{0xCA}, 7, 0, addr)
	err := tx.Verify()
	if !errors.Is(err, ErrOwnerThresholdExceedsAddrs) {
		t.Fatalf("Verify(threshold=7) = %v, want ErrOwnerThresholdExceedsAddrs", err)
	}
}

// TestTransferChainOwnershipTx_Verify_AdversarialWireBuffer pins R6V8
// against the strongest threat model: adversary controls the wire byte
// stream. Build a legitimate tx, patch the threshold byte in-place to 0,
// re-Wrap, and prove Verify rejects.
func TestTransferChainOwnershipTx_Verify_AdversarialWireBuffer(t *testing.T) {
	addr := ids.ShortID{0x77}
	good := NewTransferChainOwnershipTx(ids.ID{0xCA, 0xCA, 0xCA, 0xCA}, 1, 0, addr)
	buf := good.Bytes()

	// Re-parse so we can probe the live root.Uint32 location, then patch.
	msg, err := zap.Parse(buf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := msg.Root().Uint32(OffsetTransferChainOwnershipTx_OwnerThreshold); got != 1 {
		t.Fatalf("pre-overwrite threshold = %d, want 1", got)
	}

	tampered := make([]byte, len(buf))
	copy(tampered, buf)

	// Unique pattern: Chain field begins 0xCA,0xCA,0xCA,0xCA at
	// OffsetTransferChainOwnershipTx_Chain (=1) within the root payload.
	// Threshold uint32 (LE) = {0x01,0,0,0} sits 32 bytes after the chain
	// start. Locktime uint64 (LE) zeros for 8 bytes, then 0x77 marks
	// OwnerAddress[0].
	patched := false
	// Search bound: we need bytes i..i+44 to be the (Chain, threshold,
	// locktime, addr[0]) span — that's 45 bytes, so i+45 <= len(tampered)
	// is sufficient. SizeTransferChainOwnershipTx (=65) would over-clamp
	// when the tx sits at the buffer's tail (no trailing slack).
	for i := 0; i+45 <= len(tampered); i++ {
		if tampered[i] != 0xCA || tampered[i+1] != 0xCA ||
			tampered[i+2] != 0xCA || tampered[i+3] != 0xCA {
			continue
		}
		// Threshold at +32.
		if tampered[i+32] != 0x01 || tampered[i+33] != 0 ||
			tampered[i+34] != 0 || tampered[i+35] != 0 {
			continue
		}
		// Locktime at +36 should be 8 zero bytes.
		zero := true
		for j := 36; j < 44; j++ {
			if tampered[i+j] != 0 {
				zero = false
				break
			}
		}
		if !zero {
			continue
		}
		// OwnerAddress[0] at +44.
		if tampered[i+44] != 0x77 {
			continue
		}
		// Patch threshold to 0.
		tampered[i+32] = 0
		patched = true
		break
	}
	if !patched {
		t.Fatalf("could not locate threshold field in wire buffer")
	}
	tamperedTx, err := WrapTransferChainOwnershipTx(tampered)
	if err != nil {
		t.Fatalf("WrapTransferChainOwnershipTx(tampered) = %v, want nil (parser permissive)", err)
	}
	if tamperedTx.OwnerThreshold() != 0 {
		t.Fatalf("tampered threshold = %d, want 0", tamperedTx.OwnerThreshold())
	}
	if err := tamperedTx.Verify(); !errors.Is(err, ErrOwnerThresholdZero) {
		t.Fatalf("Verify(tampered) = %v, want ErrOwnerThresholdZero", err)
	}
}

// TestCreateSovereignL1Tx_Verify_AdversarialWireBuffer_ValidatorWeight
// pins R6V4 against wire tampering: build a legitimate tx then patch
// the second validator's Weight to 0 in the wire buffer.
func TestCreateSovereignL1Tx_Verify_AdversarialWireBuffer_ValidatorWeight(t *testing.T) {
	stub := OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x42}}
	v0 := makeValidVerifyingValidator(t)
	v1 := makeValidVerifyingValidator(t)
	// Make v1's NodeID uniquely identifiable so we can find its record.
	v1.NodeID = ids.NodeID{0xDE, 0xAD, 0xBE, 0xEF}
	v1.Weight = 0x4242_4242 // Will patch to 0
	tx := NewCreateSovereignL1Tx(CreateSovereignL1TxInput{
		NetworkID:  1,
		Owner:      stub,
		Validators: []ValidatorsListEntry{v0, v1},
		Chains:     []ChainsListEntry{makeValidChainsEntry()},
	})
	// Sanity: baseline Verify passes.
	if err := tx.Verify(); err != nil {
		t.Fatalf("baseline Verify = %v, want nil", err)
	}

	buf := tx.Bytes()
	tampered := make([]byte, len(buf))
	copy(tampered, buf)
	patched := false
	for i := 0; i+SizeValidatorRecord <= len(tampered); i++ {
		// Look for NodeID 0xDE,0xAD,0xBE,0xEF prefix.
		if tampered[i] != 0xDE || tampered[i+1] != 0xAD ||
			tampered[i+2] != 0xBE || tampered[i+3] != 0xEF {
			continue
		}
		// NodeID is 20 bytes — rest should be zero per the constructor's
		// ids.NodeID{0xDE,0xAD,0xBE,0xEF} layout.
		zeroTail := true
		for j := 4; j < 20; j++ {
			if tampered[i+j] != 0 {
				zeroTail = false
				break
			}
		}
		if !zeroTail {
			continue
		}
		// Weight at +20 (LE uint64) should be 0x4242_4242 = {0x42,0x42,0x42,0x42,0,0,0,0}.
		if tampered[i+20] != 0x42 || tampered[i+21] != 0x42 ||
			tampered[i+22] != 0x42 || tampered[i+23] != 0x42 {
			continue
		}
		// Patch Weight to 0.
		for j := 20; j < 28; j++ {
			tampered[i+j] = 0
		}
		patched = true
		break
	}
	if !patched {
		t.Fatalf("could not locate validator weight in wire buffer")
	}
	tamperedTx, err := WrapCreateSovereignL1Tx(tampered)
	if err != nil {
		t.Fatalf("WrapCreateSovereignL1Tx(tampered) = %v, want nil (parser permissive)", err)
	}
	if rec := tamperedTx.Validators().At(1); rec.Weight() != 0 {
		t.Fatalf("tampered Weight = %d, want 0", rec.Weight())
	}
	if err := tamperedTx.Verify(); !errors.Is(err, ErrValidatorWeightZero) {
		t.Fatalf("Verify(tampered weight=0) = %v, want ErrValidatorWeightZero", err)
	}
}

// Sanity: confirm the BLS package surface we depend on is reachable at
// link time (catches a future luxfi/crypto refactor that moves the
// VerifyProofOfPossession symbol).
func TestBLSSurfaceReachable(t *testing.T) {
	_ = bls.VerifyProofOfPossession
}
