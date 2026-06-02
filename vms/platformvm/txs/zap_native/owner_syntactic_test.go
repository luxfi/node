// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"errors"
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// TestOwnerSyntacticVerifyAcceptsValid pins the happy path: a well-formed
// multi-address Owner with threshold ∈ [1, len(Addresses)] returns nil.
//
// LP-023 Red round 4 finding R4V7: the wire layer accepts threshold > len
// and threshold == 0; the executor's call-site SyntacticVerify must reject.
func TestOwnerSyntacticVerifyAcceptsValid(t *testing.T) {
	addrs := []ids.ShortID{
		{0x01}, {0x02}, {0x03},
	}
	b := zap.NewBuilder(256)
	thr, lt, off, count, err := NewOwnerInline(b, OwnerInput{
		Threshold: 2,
		Locktime:  1_700_000_000,
		Addresses: addrs,
	})
	if err != nil {
		t.Fatalf("NewOwnerInline: %v", err)
	}
	ob := b.StartObject(SizeOwnerHeader)
	ob.SetUint32(OffsetOwnerHeader_Threshold, thr)
	ob.SetUint64(OffsetOwnerHeader_Locktime, lt)
	ob.SetList(OffsetOwnerHeader_AddressList, off, count)
	ob.FinishAsRoot()
	msg, err := zap.Parse(b.Finish())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	owner := OwnerView(msg.Root(), 0)
	if err := owner.SyntacticVerify(); err != nil {
		t.Fatalf("SyntacticVerify(valid) = %v, want nil", err)
	}
}

// TestOwnerSyntacticVerifyRejectsThresholdZero pins R4V7: threshold == 0
// means "any signer is fine, no quorum needed" — a security gap that
// effectively disables authorization. SyntacticVerify must reject with
// the typed error ErrOwnerThresholdZero.
//
// Attack: an adversary publishes a wire-encoded Owner with threshold=0
// and len(Addresses)=k. Naive consumers calling Threshold() see 0 and
// either treat the gate as "open" or trigger underflow in
// quorum-counting code. Both flows skip signature validation.
func TestOwnerSyntacticVerifyRejectsThresholdZero(t *testing.T) {
	addrs := []ids.ShortID{{0x01}, {0x02}}
	b := zap.NewBuilder(256)
	// Bypass NewOwnerInline (which would also reject 0 in the future) and
	// hand-craft the malicious wire shape directly — this models the
	// adversary's untrusted-network buffer.
	off, count := WriteAddressList(b, addrs)
	ob := b.StartObject(SizeOwnerHeader)
	ob.SetUint32(OffsetOwnerHeader_Threshold, 0)
	ob.SetUint64(OffsetOwnerHeader_Locktime, 0)
	ob.SetList(OffsetOwnerHeader_AddressList, off, count)
	ob.FinishAsRoot()
	msg, err := zap.Parse(b.Finish())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	owner := OwnerView(msg.Root(), 0)
	if got := owner.SyntacticVerify(); !errors.Is(got, ErrOwnerThresholdZero) {
		t.Fatalf("SyntacticVerify(threshold=0) = %v, want ErrOwnerThresholdZero", got)
	}
}

// TestOwnerSyntacticVerifyRejectsThresholdExceedsAddrs pins R4V7: threshold
// > len(Addresses) is unsatisfiable. SyntacticVerify must reject with
// ErrOwnerThresholdExceedsAddrs.
//
// Attack: adversary sets threshold=1000 against len(addrs)=2. The wire
// layer accepts (no cross-field constraint). A consumer that trusts the
// threshold as a quorum count without bounding against Len() will
// either always reject (DoS — locks the spend forever) or, worse if the
// quorum counter is tracked with signed arithmetic, can be underflowed.
func TestOwnerSyntacticVerifyRejectsThresholdExceedsAddrs(t *testing.T) {
	addrs := []ids.ShortID{{0x01}, {0x02}}
	b := zap.NewBuilder(256)
	off, count := WriteAddressList(b, addrs)
	ob := b.StartObject(SizeOwnerHeader)
	ob.SetUint32(OffsetOwnerHeader_Threshold, 7) // > 2 addrs
	ob.SetUint64(OffsetOwnerHeader_Locktime, 0)
	ob.SetList(OffsetOwnerHeader_AddressList, off, count)
	ob.FinishAsRoot()
	msg, err := zap.Parse(b.Finish())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	owner := OwnerView(msg.Root(), 0)
	if got := owner.SyntacticVerify(); !errors.Is(got, ErrOwnerThresholdExceedsAddrs) {
		t.Fatalf("SyntacticVerify(threshold=7,addrs=2) = %v, want ErrOwnerThresholdExceedsAddrs", got)
	}
}

// TestOwnerSyntacticVerifyRejectsEmptyAddrs pins R4V7: a wire-encoded
// Owner with an empty AddressList is undefined — there's no signer set,
// so authorization is undefined. Reject with ErrOwnerAddrsEmpty.
//
// Attack: adversary sets the wire AddressList to length 0. The wire
// layer is happy (Len() returns 0 honestly). A naive consumer iterating
// the list with for i:=0;i<Len();i++ never enters the loop, and if it
// then checks "did we accumulate threshold sigs?" with threshold=0
// (R4V7 also covers that), the spend goes through with zero signatures.
func TestOwnerSyntacticVerifyRejectsEmptyAddrs(t *testing.T) {
	b := zap.NewBuilder(64)
	ob := b.StartObject(SizeOwnerHeader)
	ob.SetUint32(OffsetOwnerHeader_Threshold, 1)
	ob.SetUint64(OffsetOwnerHeader_Locktime, 0)
	// No SetList — leaves the AddressList pointer as null (Len()=0).
	ob.FinishAsRoot()
	msg, err := zap.Parse(b.Finish())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	owner := OwnerView(msg.Root(), 0)
	if got := owner.SyntacticVerify(); !errors.Is(got, ErrOwnerAddrsEmpty) {
		t.Fatalf("SyntacticVerify(empty addrs) = %v, want ErrOwnerAddrsEmpty", got)
	}
}

// TestOwnerStubSyntacticVerifyAcceptsValid pins the single-address fast
// path: threshold=1, address non-zero → SyntacticVerify returns nil.
func TestOwnerStubSyntacticVerifyAcceptsValid(t *testing.T) {
	stub := OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x42}}
	if err := stub.SyntacticVerify(); err != nil {
		t.Fatalf("SyntacticVerify(valid stub) = %v, want nil", err)
	}
}

// TestOwnerStubSyntacticVerifyRejectsThresholdZero pins the single-address
// stub gate: threshold=0 must be rejected (parallel of the multi-address
// Owner case — same security gap).
func TestOwnerStubSyntacticVerifyRejectsThresholdZero(t *testing.T) {
	stub := OwnerStub{Threshold: 0, Locktime: 0, Address: ids.ShortID{0x42}}
	if got := stub.SyntacticVerify(); !errors.Is(got, ErrOwnerThresholdZero) {
		t.Fatalf("SyntacticVerify(threshold=0 stub) = %v, want ErrOwnerThresholdZero", got)
	}
}

// TestOwnerStubSyntacticVerifyRejectsThresholdAboveOne pins R4V7 for the
// stub: by construction the stub has exactly ONE address, so threshold
// values > 1 are unsatisfiable.
func TestOwnerStubSyntacticVerifyRejectsThresholdAboveOne(t *testing.T) {
	stub := OwnerStub{Threshold: 2, Locktime: 0, Address: ids.ShortID{0x42}}
	if got := stub.SyntacticVerify(); !errors.Is(got, ErrOwnerThresholdExceedsAddrs) {
		t.Fatalf("SyntacticVerify(threshold=2 stub) = %v, want ErrOwnerThresholdExceedsAddrs", got)
	}
}
