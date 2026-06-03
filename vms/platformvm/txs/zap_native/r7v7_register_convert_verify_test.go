// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"errors"
	"testing"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/ids"
)

// LP-023 Red round 7 R7V7 — Verify() tests for RegisterL1ValidatorTx +
// ConvertNetworkToL1Tx.
//
// Both tx types carry BLS PoP fields that the wire layer cannot prove
// pair-correct. Blue's 8-tx Verify() list excluded them; the gate fires
// even though Path A (R7V5 admission gate) refuses these kinds at the
// mempool boundary today. Defense-in-depth: when the executor lands
// (batch 6+) and the admission gate is lifted, the Verify() path is
// already canonical.

// makeKnownGoodBLS mints a real BLS keypair and returns the
// (pkBytes, popBytes) pair that bls.VerifyProofOfPossession accepts.
// Tests use this when they want Verify() to succeed on the BLS gate.
func makeKnownGoodBLS(t *testing.T) (pkBytes [BLSPubKeySize]byte, popBytes [BLSPoPSize]byte) {
	t.Helper()
	sk, err := bls.NewSecretKey()
	if err != nil {
		t.Fatalf("bls.NewSecretKey: %v", err)
	}
	ls, err := localsigner.FromBytes(bls.SecretKeyToBytes(sk))
	if err != nil {
		t.Fatalf("localsigner.FromBytes: %v", err)
	}
	pk := ls.PublicKey()
	pkRaw := bls.PublicKeyToCompressedBytes(pk)
	sig, err := ls.SignProofOfPossession(pkRaw)
	if err != nil {
		t.Fatalf("SignProofOfPossession: %v", err)
	}
	sigRaw := bls.SignatureToBytes(sig)
	copy(pkBytes[:], pkRaw)
	copy(popBytes[:], sigRaw)
	return pkBytes, popBytes
}

// makeMismatchedBLS mints two independent BLS keypairs and crosses them
// — returns pk from keypair 1 paired with a PoP signed by keypair 2.
// The pairing check MUST fail.
func makeMismatchedBLS(t *testing.T) (pkBytes [BLSPubKeySize]byte, popBytes [BLSPoPSize]byte) {
	t.Helper()
	pk1, _ := makeKnownGoodBLS(t)
	_, pop2 := makeKnownGoodBLS(t)
	return pk1, pop2
}

// TestRegisterL1ValidatorTx_Verify_RejectsBadBLSPoP pins R7V7: a
// RegisterL1ValidatorTx with a BLS PoP that doesn't pair with the
// BLS pubkey must be refused by Verify().
func TestRegisterL1ValidatorTx_Verify_RejectsBadBLSPoP(t *testing.T) {
	pk, badPop := makeMismatchedBLS(t)
	tx := NewRegisterL1ValidatorTx(
		ids.ID{0xAA},
		pk,
		badPop, // adversary substituted a PoP from a different key
		1_900_000_000,
		ids.ID{0xBB},
	)
	err := tx.Verify()
	if !errors.Is(err, ErrBadBLSPoP) {
		t.Fatalf("Verify(mismatched BLS) = %v, want ErrBadBLSPoP", err)
	}
}

// TestRegisterL1ValidatorTx_Verify_RejectsMalformedBLSPubKey pins R7V7:
// a BLS pubkey that fails PublicKeyFromCompressedBytes (e.g. all zero
// or non-canonical encoding) must be refused by Verify().
func TestRegisterL1ValidatorTx_Verify_RejectsMalformedBLSPubKey(t *testing.T) {
	var pk [BLSPubKeySize]byte
	// All-zero pubkey is not a valid compressed-G1 encoding.
	_, validPop := makeKnownGoodBLS(t)
	tx := NewRegisterL1ValidatorTx(
		ids.ID{0xAA},
		pk,
		validPop,
		1_900_000_000,
		ids.ID{0xBB},
	)
	err := tx.Verify()
	if !errors.Is(err, ErrBadBLSPoP) {
		t.Fatalf("Verify(malformed pk) = %v, want ErrBadBLSPoP", err)
	}
}

// TestRegisterL1ValidatorTx_Verify_AcceptsValidPoP pins R7V7 positive
// path: a well-formed pubkey + PoP pair MUST pass Verify().
func TestRegisterL1ValidatorTx_Verify_AcceptsValidPoP(t *testing.T) {
	pk, pop := makeKnownGoodBLS(t)
	tx := NewRegisterL1ValidatorTx(
		ids.ID{0xAA},
		pk,
		pop,
		1_900_000_000,
		ids.ID{0xBB},
	)
	if err := tx.Verify(); err != nil {
		t.Fatalf("Verify(valid pk+pop) = %v, want nil", err)
	}
}

// TestRegisterL1ValidatorTx_Verify_RejectsZeroExpiry pins R7V7: an
// Expiry of zero is never a legitimate registration window. Full
// timestamp-vs-now() check lives in the executor; this is the
// syntactic floor.
func TestRegisterL1ValidatorTx_Verify_RejectsZeroExpiry(t *testing.T) {
	pk, pop := makeKnownGoodBLS(t)
	tx := NewRegisterL1ValidatorTx(
		ids.ID{0xAA},
		pk,
		pop,
		0, // adversarial zero expiry
		ids.ID{0xBB},
	)
	err := tx.Verify()
	if !errors.Is(err, ErrZeroExpiry) {
		t.Fatalf("Verify(zero expiry) = %v, want ErrZeroExpiry", err)
	}
}

// TestRegisterL1ValidatorTx_Verify_AdversarialWireBuffer pins R7V7
// against the strongest threat model: adversary controls the wire byte
// stream. We construct a legitimate tx then patch the Expiry bytes to
// zero in the wire buffer and re-wrap. Verify() must catch it.
func TestRegisterL1ValidatorTx_Verify_AdversarialWireBuffer(t *testing.T) {
	pk, pop := makeKnownGoodBLS(t)
	tx := NewRegisterL1ValidatorTx(
		ids.ID{0xAA},
		pk,
		pop,
		1_900_000_000,
		ids.ID{0xBB},
	)
	if err := tx.Verify(); err != nil {
		t.Fatalf("baseline Verify = %v, want nil", err)
	}

	buf := tx.Bytes()
	// Locate Expiry by its known value. The constructor emits the
	// fixed section at the root object; Expiry is at
	// OffsetRegisterL1ValidatorTx_Expiry within that object. We can't
	// know the exact root-object byte offset in the parent header
	// without re-parsing — so we search for the known little-endian
	// encoding of 1_900_000_000 (0x713FB300 in hex) and zero its bytes.
	//
	// 1_900_000_000 decimal = 0x713FB300 (LE bytes: 00 B3 3F 71 00 00 00 00).
	const target0 = 0x00
	const target1 = 0xB3
	const target2 = 0x3F
	const target3 = 0x71
	found := false
	for i := 0; i+8 <= len(buf); i++ {
		if buf[i] == target0 && buf[i+1] == target1 &&
			buf[i+2] == target2 && buf[i+3] == target3 &&
			buf[i+4] == 0 && buf[i+5] == 0 && buf[i+6] == 0 && buf[i+7] == 0 {
			// Zero the 8 bytes in place.
			for j := 0; j < 8; j++ {
				buf[i+j] = 0
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("could not locate Expiry bytes in wire buffer")
	}

	tampered, err := WrapRegisterL1ValidatorTx(buf)
	if err != nil {
		t.Fatalf("WrapRegisterL1ValidatorTx(tampered) = %v, want nil", err)
	}
	if got := tampered.Expiry(); got != 0 {
		t.Fatalf("tampered Expiry = %d, want 0", got)
	}
	if err := tampered.Verify(); !errors.Is(err, ErrZeroExpiry) {
		t.Fatalf("Verify(tampered zero-expiry) = %v, want ErrZeroExpiry", err)
	}
}

// TestConvertNetworkToL1Tx_Verify_RejectsZeroValidators pins R7V7:
// an empty Validators sub-list breaks consensus on the new L1 (no
// quorum can ever form). Verify must reject at the wire boundary.
func TestConvertNetworkToL1Tx_Verify_RejectsZeroValidators(t *testing.T) {
	tx := NewConvertNetworkToL1Tx(ConvertNetworkToL1TxInput{
		NetworkID:      1,
		BlockchainID:   ids.ID{0x11},
		Chain:          ids.ID{0x22},
		ManagerChainID: ids.ID{0x33},
		Address:        []byte("0x00"),
		Validators:     nil, // adversarial empty set
	})
	err := tx.Verify()
	if !errors.Is(err, ErrZeroValidators) {
		t.Fatalf("Verify(zero validators) = %v, want ErrZeroValidators", err)
	}
}

// TestConvertNetworkToL1Tx_Verify_RejectsZeroWeight pins R7V7: a
// per-validator Weight of zero skews quorum (filler entries that pad
// the count but contribute nothing to the threshold).
func TestConvertNetworkToL1Tx_Verify_RejectsZeroWeight(t *testing.T) {
	pk, pop := makeKnownGoodBLS(t)
	tx := NewConvertNetworkToL1Tx(ConvertNetworkToL1TxInput{
		NetworkID:      1,
		BlockchainID:   ids.ID{0x11},
		Chain:          ids.ID{0x22},
		ManagerChainID: ids.ID{0x33},
		Address:        []byte("0x00"),
		Validators: []ValidatorsListEntry{
			{
				NodeID:             ids.NodeID{0x77},
				Weight:             0, // adversarial zero weight
				BLSPubKey:          pk,
				BLSPoP:             pop,
				RegistrationExpiry: 1_900_000_000,
			},
		},
	})
	err := tx.Verify()
	if !errors.Is(err, ErrValidatorWeightZero) {
		t.Fatalf("Verify(zero weight) = %v, want ErrValidatorWeightZero", err)
	}
}

// TestConvertNetworkToL1Tx_Verify_RejectsBadBLSPoP pins R7V7: a
// per-validator BLS PoP that doesn't pair with the BLS pubkey is the
// authority-substitution primitive that the gate must block.
func TestConvertNetworkToL1Tx_Verify_RejectsBadBLSPoP(t *testing.T) {
	pk, badPop := makeMismatchedBLS(t)
	tx := NewConvertNetworkToL1Tx(ConvertNetworkToL1TxInput{
		NetworkID:      1,
		BlockchainID:   ids.ID{0x11},
		Chain:          ids.ID{0x22},
		ManagerChainID: ids.ID{0x33},
		Address:        []byte("0x00"),
		Validators: []ValidatorsListEntry{
			{
				NodeID:             ids.NodeID{0x77},
				Weight:             1_000_000,
				BLSPubKey:          pk,
				BLSPoP:             badPop,
				RegistrationExpiry: 1_900_000_000,
			},
		},
	})
	err := tx.Verify()
	if !errors.Is(err, ErrBadBLSPoP) {
		t.Fatalf("Verify(mismatched BLS) = %v, want ErrBadBLSPoP", err)
	}
}

// TestConvertNetworkToL1Tx_Verify_AcceptsValidValidators pins R7V7
// positive path: a well-formed validators set MUST pass Verify().
func TestConvertNetworkToL1Tx_Verify_AcceptsValidValidators(t *testing.T) {
	pk, pop := makeKnownGoodBLS(t)
	tx := NewConvertNetworkToL1Tx(ConvertNetworkToL1TxInput{
		NetworkID:      1,
		BlockchainID:   ids.ID{0x11},
		Chain:          ids.ID{0x22},
		ManagerChainID: ids.ID{0x33},
		Address:        []byte("0x00"),
		Validators: []ValidatorsListEntry{
			{
				NodeID:             ids.NodeID{0x77},
				Weight:             1_000_000,
				BLSPubKey:          pk,
				BLSPoP:             pop,
				RegistrationExpiry: 1_900_000_000,
			},
		},
	})
	if err := tx.Verify(); err != nil {
		t.Fatalf("Verify(valid) = %v, want nil", err)
	}
}

// TestConvertNetworkToL1Tx_Verify_AdversarialWireBuffer pins R7V7
// against the strongest threat model: adversary controls the wire
// byte stream. We construct a legitimate tx then zero the first
// validator's Weight bytes in the wire buffer and re-wrap. Verify()
// must catch it.
func TestConvertNetworkToL1Tx_Verify_AdversarialWireBuffer(t *testing.T) {
	pk, pop := makeKnownGoodBLS(t)
	const goodWeight = uint64(1_000_000)
	tx := NewConvertNetworkToL1Tx(ConvertNetworkToL1TxInput{
		NetworkID:      1,
		BlockchainID:   ids.ID{0x11},
		Chain:          ids.ID{0x22},
		ManagerChainID: ids.ID{0x33},
		Address:        []byte("0x00"),
		Validators: []ValidatorsListEntry{
			{
				NodeID:             ids.NodeID{0x77},
				Weight:             goodWeight,
				BLSPubKey:          pk,
				BLSPoP:             pop,
				RegistrationExpiry: 1_900_000_000,
			},
		},
	})
	if err := tx.Verify(); err != nil {
		t.Fatalf("baseline Verify = %v, want nil", err)
	}

	buf := tx.Bytes()
	// 1_000_000 decimal = 0xF4240 → LE bytes: 40 42 0F 00 00 00 00 00.
	// Search for the canonical 8-byte LE encoding and zero it.
	const target0 = 0x40
	const target1 = 0x42
	const target2 = 0x0F
	found := false
	for i := 0; i+8 <= len(buf); i++ {
		if buf[i] == target0 && buf[i+1] == target1 &&
			buf[i+2] == target2 && buf[i+3] == 0 &&
			buf[i+4] == 0 && buf[i+5] == 0 && buf[i+6] == 0 && buf[i+7] == 0 {
			for j := 0; j < 8; j++ {
				buf[i+j] = 0
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("could not locate Weight bytes in wire buffer")
	}

	tampered, err := WrapConvertNetworkToL1Tx(buf)
	if err != nil {
		t.Fatalf("WrapConvertNetworkToL1Tx(tampered) = %v, want nil", err)
	}
	if got := tampered.Validators().At(0).Weight(); got != 0 {
		t.Fatalf("tampered Weight = %d, want 0", got)
	}
	if err := tampered.Verify(); !errors.Is(err, ErrValidatorWeightZero) {
		t.Fatalf("Verify(tampered zero-weight) = %v, want ErrValidatorWeightZero", err)
	}
}
