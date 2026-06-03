// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// ValidatorRecord is the fixed-stride wire entry of a ValidatorsList. One
// initial validator per record carrying:
//
//   - NodeID                 20 B (ids.NodeID)
//   - Weight                  8 B (uint64; stake amount)
//   - BLSPubKey              48 B (BLS12-381 G1 compressed)
//   - BLSPoP                 96 B (BLS proof-of-possession, G2 compressed)
//   - RegistrationExpiry      8 B (unix timestamp; validator must register
//                                  on the new L1 before this expiry)
//   = 180 B per record (no sibling arrays needed; everything fixed-size).
//
// Wire layout per ValidatorRecord (stride 180 bytes; 4-byte aligned):
//
//	NodeID                  20B    @ 0
//	Weight                  uint64 @ 20
//	BLSPubKey               48B    @ 28
//	BLSPoP                  96B    @ 76
//	RegistrationExpiry      uint64 @ 172
//
// Total = 180 bytes.
const (
	OffsetValidatorRecord_NodeID             = 0
	OffsetValidatorRecord_Weight             = 20
	OffsetValidatorRecord_BLSPubKey          = 28
	OffsetValidatorRecord_BLSPoP             = 76
	OffsetValidatorRecord_RegistrationExpiry = 172
	SizeValidatorRecord                      = 180

	BLSPubKeySize = 48
	BLSPoPSize    = 96

	// MaxValidatorsPerL1 is the hard cap on the number of initial
	// validators a CreateSovereignL1Tx or ConvertNetworkToL1Tx may
	// declare. 1024 matches the practical upper bound on initial-
	// validator sets for an L1 spawn and limits the worst-case BLS
	// pairing walk (O(N) expensive, fires per validator at admission).
	//
	// MustVerify rejects any wire-encoded ValidatorsList with Len() >
	// this value at the boundary so an adversary cannot bypass the cap
	// by declaring validators they don't intend to bootstrap.
	//
	// LP-023 batch 5 v3.7 (paired with MaxChainsPerL1).
	MaxValidatorsPerL1 = 1024
)

// ValidatorRecord is the zero-copy WIRE view over one initial-validator
// record. All fields are fixed-size so there is no Bind() step — every
// accessor is safe to call directly.
//
// CONSUMER SAFETY: BLSPubKey and BLSPoP are returned as COPIED []byte
// (not aliasing the parent buffer). Consumers may mutate the returned
// slices without corrupting subsequent reads. The signature verification
// (BLS pairing) is the consumer's responsibility.
//
// ZERO-VALUE SAFETY: `ValidatorRecord{}` (returned by ValidatorsList.At
// on out-of-range / clamped lists) is detectable via IsNull(); calling
// any accessor on a zero-value record returns the zero value (NodeID=0,
// Weight=0, empty BLS fields) instead of panicking.
type ValidatorRecord struct {
	obj zap.Object
}

// IsNull returns true if the record is the zero value. Accessors on a
// zero-value record return zero rather than panic — callers may still
// want to short-circuit downstream work on IsNull records.
func (r ValidatorRecord) IsNull() bool { return r.obj.IsNull() }

// NodeID returns the validator's NodeID. Returns zero on a zero-value record.
func (r ValidatorRecord) NodeID() ids.NodeID {
	if r.obj.IsNull() {
		return ids.NodeID{}
	}
	var out ids.NodeID
	for i := 0; i < 20; i++ {
		out[i] = r.obj.Uint8(OffsetValidatorRecord_NodeID + i)
	}
	return out
}

// Weight returns the validator's stake weight. Returns 0 on a zero-value record.
func (r ValidatorRecord) Weight() uint64 {
	if r.obj.IsNull() {
		return 0
	}
	return r.obj.Uint64(OffsetValidatorRecord_Weight)
}

// BLSPubKey returns the validator's BLS public key (G1 compressed, 48B).
// The returned slice is a COPY — consumer mutation does not affect
// subsequent reads or the underlying ZAP buffer.
// Returns an empty slice on a zero-value record.
func (r ValidatorRecord) BLSPubKey() []byte {
	if r.obj.IsNull() {
		return make([]byte, BLSPubKeySize)
	}
	out := make([]byte, BLSPubKeySize)
	for i := 0; i < BLSPubKeySize; i++ {
		out[i] = r.obj.Uint8(OffsetValidatorRecord_BLSPubKey + i)
	}
	return out
}

// BLSPoP returns the validator's BLS proof-of-possession (G2 compressed,
// 96B). The PoP binds the validator's BLS keypair to their NodeID and
// must be verified before registering the validator on the L1.
// The returned slice is a COPY — consumer mutation does not affect
// subsequent reads or the underlying ZAP buffer.
// Returns an empty slice on a zero-value record.
func (r ValidatorRecord) BLSPoP() []byte {
	if r.obj.IsNull() {
		return make([]byte, BLSPoPSize)
	}
	out := make([]byte, BLSPoPSize)
	for i := 0; i < BLSPoPSize; i++ {
		out[i] = r.obj.Uint8(OffsetValidatorRecord_BLSPoP + i)
	}
	return out
}

// RegistrationExpiry returns the unix timestamp by which the validator
// must register on the new L1. If the L1 hasn't accepted the registration
// by this time, the slot expires. Returns 0 on a zero-value record.
func (r ValidatorRecord) RegistrationExpiry() uint64 {
	if r.obj.IsNull() {
		return 0
	}
	return r.obj.Uint64(OffsetValidatorRecord_RegistrationExpiry)
}

// IsBLSPubKeyZero walks the 48B BLSPubKey field byte-by-byte via
// Object.Uint8(offset) without allocating. Returns true iff every byte
// is zero. Returns true on a zero-value record.
//
// LP-023 batch 5 v3.8 R4 (Red round 6 V4): the structural floor check
// in ValidatorsList.MustVerify previously allocated 48B+96B per
// validator via BLSPubKey()/BLSPoP() — amplification at N=1024 was
// ~144KB on all-zero rejection. The zero-scan path keeps the floor
// gate O(1) allocations regardless of N. The allocating accessor is
// retained for the BLS pairing path in tx_verify.go where the verifier
// requires a copy.
func (r ValidatorRecord) IsBLSPubKeyZero() bool {
	if r.obj.IsNull() {
		return true
	}
	for i := 0; i < BLSPubKeySize; i++ {
		if r.obj.Uint8(OffsetValidatorRecord_BLSPubKey+i) != 0 {
			return false
		}
	}
	return true
}

// IsBLSPoPZero walks the 96B BLSPoP field byte-by-byte via
// Object.Uint8(offset) without allocating. Returns true iff every byte
// is zero. Returns true on a zero-value record.
//
// Companion to IsBLSPubKeyZero — see that docstring for LP-023 batch 5
// v3.8 R4 (Red round 6 V4) amplification rationale.
func (r ValidatorRecord) IsBLSPoPZero() bool {
	if r.obj.IsNull() {
		return true
	}
	for i := 0; i < BLSPoPSize; i++ {
		if r.obj.Uint8(OffsetValidatorRecord_BLSPoP+i) != 0 {
			return false
		}
	}
	return true
}

// ValidatorsList is the zero-copy WIRE view over a list of ValidatorRecord
// items. Fixed-stride only — no sibling arrays needed.
//
// Uses Object.ListStride from zap v0.7.2 for per-element clamp: a
// poisoned length field that passes the permissive baseline gets
// rejected here (R4V9, LP-023 Red round 4).
type ValidatorsList struct {
	list zap.List
}

// Len returns the validator count.
func (l ValidatorsList) Len() int { return l.list.Len() }

// IsNull returns true if no list pointer was set.
func (l ValidatorsList) IsNull() bool { return l.list.IsNull() }

// At returns the i'th ValidatorRecord. Returns zero-value ValidatorRecord
// when out of range — accessor calls on the zero value return zero
// fields rather than panicking.
func (l ValidatorsList) At(i int) ValidatorRecord {
	if i < 0 || i >= l.list.Len() {
		return ValidatorRecord{}
	}
	return ValidatorRecord{obj: l.list.Object(i, SizeValidatorRecord)}
}

// NewValidatorsListView reads a ValidatorsList from a parent object's
// field offset. Uses the per-stride clamp (R4V9).
func NewValidatorsListView(parent zap.Object, fieldOffset int) ValidatorsList {
	return ValidatorsList{list: parent.ListStride(fieldOffset, SizeValidatorRecord)}
}

// MustVerify walks every entry in the list and asserts the 5 sub-field
// invariants the wire layer cannot enforce structurally:
//
//   - Len() <= MaxValidatorsPerL1 (1024). An adversary claiming 100k
//     validators would force the per-validator BLS pairing walk
//     (O(N) expensive) at admission time.
//   - Weight > 0. A zero-weight validator contributes nothing to
//     quorum but pads the count — a quorum-skew primitive. Mirrors
//     the per-validator check already wired in tx_verify.go for
//     CreateSovereignL1Tx and ConvertNetworkToL1Tx; lifting it onto
//     the list-level MustVerify makes the floor invariant grep-able
//     and keeps the per-tx Verify body purely orchestration logic.
//   - BLSPubKey not all-zero. The R6V3 pairing check would catch the
//     downstream substitution, but the all-zero pubkey is a structural
//     floor invariant cheaper to check first and a clear signal of
//     malformed wire bytes rather than a cryptographic attack.
//   - BLSPoP not all-zero. Same structural floor as BLSPubKey.
//   - RegistrationExpiry > 0. Wire-canonically a unix timestamp can
//     never be zero in a legitimate registration window (parallel of
//     ErrZeroExpiry on RegisterL1ValidatorTx).
//
// Returns the first failure wrapped with the validator index. Returns
// nil when the list is empty or every entry passes — empty-list
// rejection is the caller's gate (CreateSovereignL1Tx.Verify enforces
// non-empty via ErrZeroValidators before invoking this).
//
// Receiver name MustVerify (not Verify) mirrors the ChainsList pattern
// from R7V8: makes "I forgot to call this" a grep-able regression. The
// audit gate (audit_test.go + .github/workflows/zap-audit.yml
// validatorslist-mustverify-gate) confirms every tx embedder calls it.
//
// LP-023 batch 5 v3.7.
func (l ValidatorsList) MustVerify() error {
	n := l.Len()
	if n > MaxValidatorsPerL1 {
		return fmt.Errorf("ValidatorsList.Len=%d: %w", n, ErrTooManyValidators)
	}
	// LP-023 batch 5 v3.8 R4 (Red round 6 V4): use the zero-scan
	// IsBLSPubKeyZero / IsBLSPoPZero accessors so the structural floor
	// check stays O(1) allocations regardless of N. The allocating
	// BLSPubKey() / BLSPoP() copies are retained for the BLS pairing
	// path in tx_verify.go where the verifier requires a copy.
	for i := 0; i < n; i++ {
		rec := l.At(i)
		if rec.IsNull() {
			continue
		}
		if rec.Weight() == 0 {
			return fmt.Errorf(
				"ValidatorsList[%d].Weight: %w", i, ErrValidatorWeightZero,
			)
		}
		if rec.IsBLSPubKeyZero() {
			return fmt.Errorf(
				"ValidatorsList[%d].BLSPubKey: %w", i, ErrValidatorBLSPubKeyZero,
			)
		}
		if rec.IsBLSPoPZero() {
			return fmt.Errorf(
				"ValidatorsList[%d].BLSPoP: %w", i, ErrValidatorBLSPoPZero,
			)
		}
		if rec.RegistrationExpiry() == 0 {
			return fmt.Errorf(
				"ValidatorsList[%d].RegistrationExpiry: %w",
				i, ErrValidatorRegistrationExpiryZero,
			)
		}
	}
	return nil
}

// ValidatorsListEntry is the constructor input for a ValidatorsList. Each
// entry pins one initial validator; fields map 1:1 to the wire record.
type ValidatorsListEntry struct {
	NodeID             ids.NodeID
	Weight             uint64
	BLSPubKey          [BLSPubKeySize]byte
	BLSPoP             [BLSPoPSize]byte
	RegistrationExpiry uint64
}

// WriteValidatorsList writes a fixed-stride list of ValidatorRecord entries
// at the builder's current position and returns (offset, count) suitable
// for ObjectBuilder.SetList.
//
// Per record: NodeID 20B + Weight 8B + BLSPubKey 48B + BLSPoP 96B +
// RegistrationExpiry 8B = 180B. No sibling arrays needed.
func WriteValidatorsList(b *zap.Builder, entries []ValidatorsListEntry) (offset, count int) {
	if len(entries) == 0 {
		return 0, 0
	}
	lb := b.StartList(SizeValidatorRecord)
	for _, e := range entries {
		var rec [SizeValidatorRecord]byte
		for i := 0; i < 20; i++ {
			rec[OffsetValidatorRecord_NodeID+i] = e.NodeID[i]
		}
		putU64(rec[OffsetValidatorRecord_Weight:], e.Weight)
		for i := 0; i < BLSPubKeySize; i++ {
			rec[OffsetValidatorRecord_BLSPubKey+i] = e.BLSPubKey[i]
		}
		for i := 0; i < BLSPoPSize; i++ {
			rec[OffsetValidatorRecord_BLSPoP+i] = e.BLSPoP[i]
		}
		putU64(rec[OffsetValidatorRecord_RegistrationExpiry:], e.RegistrationExpiry)
		lb.AddBytes(rec[:])
	}
	off, _ := lb.Finish()
	return off, len(entries)
}
