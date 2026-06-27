// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainadapter

import (
	"fmt"
	"testing"
	"time"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
)

// newTestCommittee builds an n-member committee with fresh BLS keypairs and
// returns the committee roster alongside the members' secret keys (parallel
// to committee.Members / committee.PublicKeys by index).
func newTestCommittee(t *testing.T, n, threshold int) (*ComputeCommittee, []*bls.SecretKey) {
	t.Helper()
	members := make([][]byte, n)
	pubkeys := make([][]byte, n)
	sks := make([]*bls.SecretKey, n)
	for i := 0; i < n; i++ {
		sk, err := bls.NewSecretKey()
		if err != nil {
			t.Fatalf("bls keygen: %v", err)
		}
		sks[i] = sk
		pubkeys[i] = bls.PublicKeyToCompressedBytes(bls.PublicFromSecretKey(sk))
		members[i] = []byte(fmt.Sprintf("member-%d", i))
	}
	committee := &ComputeCommittee{
		ID:         ids.ID{0xC0, 0x11, 0xEE, 0x77},
		Members:    members,
		Threshold:  threshold,
		PublicKeys: pubkeys,
	}
	return committee, sks
}

// newSignedCert returns a certificate endorsed by the committee members at the
// given indices, each signing the certificate's canonical digest with its key.
func newSignedCert(t *testing.T, committee *ComputeCommittee, sks []*bls.SecretKey, indices ...int) *CommitteeCert {
	t.Helper()
	cert := &CommitteeCert{
		CommitteeID:      committee.ID,
		Threshold:        committee.Threshold,
		TotalMembers:     len(committee.Members),
		RequestID:        ids.ID{0x11, 0x22, 0x33},
		OutputCommitment: [32]byte{0xAB, 0xCD, 0xEF},
		Timestamp:        time.Unix(1_700_000_000, 0).UTC(),
	}
	msg := cert.signingDigest()
	for _, idx := range indices {
		sig, err := sks[idx].Sign(msg[:])
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		cert.Endorsements = append(cert.Endorsements, &Endorsement{
			MemberID:    committee.Members[idx],
			MemberIndex: idx,
			Signature:   bls.SignatureToBytes(sig),
		})
	}
	return cert
}

// TestCommitteeCertValidQuorum: a quorum of distinct, correctly-signed
// endorsements verifies.
func TestCommitteeCertValidQuorum(t *testing.T) {
	committee, sks := newTestCommittee(t, 5, 3)
	cert := newSignedCert(t, committee, sks, 0, 1, 2)
	if err := cert.Verify(committee); err != nil {
		t.Fatalf("expected valid quorum to verify, got %v", err)
	}
	// A full set (all members) must also verify.
	full := newSignedCert(t, committee, sks, 0, 1, 2, 3, 4)
	if err := full.Verify(committee); err != nil {
		t.Fatalf("expected full endorsement set to verify, got %v", err)
	}
}

// TestCommitteeCertSubThreshold: fewer than Threshold endorsements is rejected.
func TestCommitteeCertSubThreshold(t *testing.T) {
	committee, sks := newTestCommittee(t, 5, 3)
	cert := newSignedCert(t, committee, sks, 0, 1) // only 2 < 3
	if err := cert.Verify(committee); err == nil {
		t.Fatal("expected sub-threshold cert to be REJECTED")
	}
}

// TestCommitteeCertDuplicateSigner: a member endorsing twice cannot inflate the
// distinct count, even though both signatures are individually valid.
func TestCommitteeCertDuplicateSigner(t *testing.T) {
	committee, sks := newTestCommittee(t, 5, 3)
	// Three endorsements but member 1 appears twice => only 2 distinct.
	cert := newSignedCert(t, committee, sks, 0, 1, 1)
	if err := cert.Verify(committee); err == nil {
		t.Fatal("expected duplicate-signer cert to be REJECTED")
	}
}

// TestCommitteeCertForgedSignature: a well-formed signature over the WRONG
// message (an attacker who controls a member key but signs a different digest)
// fails verification.
func TestCommitteeCertForgedSignature(t *testing.T) {
	committee, sks := newTestCommittee(t, 5, 3)
	cert := newSignedCert(t, committee, sks, 0, 1, 2)
	// Replace endorsement 2 with a valid signature over a different message.
	wrong, err := sks[2].Sign([]byte("not the certificate digest"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	cert.Endorsements[2].Signature = bls.SignatureToBytes(wrong)
	if err := cert.Verify(committee); err == nil {
		t.Fatal("expected forged/wrong-message signature to be REJECTED")
	}

	// Also: structurally corrupt signature bytes must be rejected, not panic.
	cert2 := newSignedCert(t, committee, sks, 0, 1, 2)
	cert2.Endorsements[1].Signature = []byte{0x00, 0x01, 0x02}
	if err := cert2.Verify(committee); err == nil {
		t.Fatal("expected malformed signature bytes to be REJECTED")
	}
}

// TestCommitteeCertUnknownSigner: an endorsement that does not correspond to a
// roster member (out-of-range index, foreign key, or mismatched identity) is
// rejected.
func TestCommitteeCertUnknownSigner(t *testing.T) {
	committee, sks := newTestCommittee(t, 5, 3)

	// (a) Member index outside the roster.
	cert := newSignedCert(t, committee, sks, 0, 1, 2)
	cert.Endorsements[2].MemberIndex = 99
	if err := cert.Verify(committee); err == nil {
		t.Fatal("expected out-of-range member index to be REJECTED")
	}

	// (b) A signer outside the committee: fresh key, but claiming a valid
	// in-range index. The signature is over the right digest but by the wrong
	// key, so it fails verification against the roster's public key.
	cert = newSignedCert(t, committee, sks, 0, 1, 2)
	foreign, err := bls.NewSecretKey()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	msg := cert.signingDigest()
	fsig, err := foreign.Sign(msg[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	cert.Endorsements[2].Signature = bls.SignatureToBytes(fsig)
	if err := cert.Verify(committee); err == nil {
		t.Fatal("expected foreign-key endorsement to be REJECTED")
	}

	// (c) Correct key & signature but MemberID does not match the roster index.
	cert = newSignedCert(t, committee, sks, 0, 1, 2)
	cert.Endorsements[2].MemberID = []byte("someone-else")
	if err := cert.Verify(committee); err == nil {
		t.Fatal("expected member-id/index mismatch to be REJECTED")
	}
}

// TestCommitteeCertParameterMismatch: threshold and committee identity must
// agree with the supplied roster.
func TestCommitteeCertParameterMismatch(t *testing.T) {
	committee, sks := newTestCommittee(t, 5, 3)

	if err := (&CommitteeCert{}).Verify(nil); err == nil {
		t.Fatal("expected nil committee to be REJECTED")
	}

	cert := newSignedCert(t, committee, sks, 0, 1, 2)
	cert.Threshold = 2 // disagrees with committee.Threshold == 3
	if err := cert.Verify(committee); err == nil {
		t.Fatal("expected threshold mismatch to be REJECTED")
	}

	cert = newSignedCert(t, committee, sks, 0, 1, 2)
	cert.CommitteeID = ids.ID{0xDE, 0xAD}
	if err := cert.Verify(committee); err == nil {
		t.Fatal("expected committee-id mismatch to be REJECTED")
	}
}

// TestVerifyResultCommitteeCert exercises the engine path: an unknown committee
// fails closed; a registered committee verifies a valid certificate.
func TestVerifyResultCommitteeCert(t *testing.T) {
	committee, sks := newTestCommittee(t, 4, 2)
	cert := newSignedCert(t, committee, sks, 0, 1)
	result := &ComputeResult{CommitteeCert: cert}

	engine := NewConfidentialComputeEngine(TEEIntelSGX)

	// Unknown committee -> fail closed.
	if err := engine.VerifyResult(result); err == nil {
		t.Fatal("expected unregistered committee to fail closed")
	}

	// After registration, a valid certificate verifies.
	if err := engine.RegisterCommittee(committee); err != nil {
		t.Fatalf("register committee: %v", err)
	}
	if err := engine.VerifyResult(result); err != nil {
		t.Fatalf("expected registered committee cert to verify, got %v", err)
	}

	// Mutating any signed field after the fact (here the certified output
	// commitment) changes the digest and invalidates every endorsement.
	tampered := newSignedCert(t, committee, sks, 0, 1)
	tampered.OutputCommitment = [32]byte{0x99} // endorsements signed over a different value
	if err := tampered.Verify(committee); err == nil {
		t.Fatal("expected endorsement over a different output to be REJECTED")
	}
}
