// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// AddressList is a variable-length list of 20-byte ids.ShortID addresses.
// Stride is ids.ShortIDLen (20). The wire layer guarantees length*stride
// fits in the message buffer via Object.ListStride (zap v0.7.2+).
//
// CONSUMER SAFETY (LP-023 Red round 4 R4V3): a malicious wire-encoded
// AddressList may report Len() > N for an actual N entries via the
// permissive ListStride clamp (Len() reflects the wire length field
// only). At(i) for i >= N returns zero-valued ShortID{} — which a naive
// "iterate every Len() index and treat every ShortID as a valid signer"
// loop would silently accept as a zero-co-signer. That zero co-signer
// can match any zero key in a buggy keystore, granting unauthorized
// spend authority.
//
// Consumers MUST:
//   1. Validate each returned address is non-zero before treating it
//      as a signer, OR
//   2. Correlate Len() against an explicit count from a sibling field
//      (e.g. parent tx's signer-count uint32), OR
//   3. Call Owner.SyntacticVerify() before iterating — it caps the
//      threshold against Len() and rejects empty lists, which closes
//      the "honest overcount" attack at the gate.
//
// Path (3) is the canonical one; the executor-side tx.Verify() entry
// point invokes Owner.SyntacticVerify().
type AddressList struct {
	list zap.List
}

// Len returns the number of addresses as reported by the wire-encoded
// length field. CONSUMER SAFETY: see AddressList type-level docstring —
// this value is attacker-controlled within the per-stride clamp.
func (a AddressList) Len() int { return a.list.Len() }

// IsNull returns true if no list pointer was set.
func (a AddressList) IsNull() bool { return a.list.IsNull() }

// At returns the i'th address. Returns the zero ShortID when out of range.
//
// CONSUMER SAFETY (R4V3): when i >= the actual entry count (a malicious
// encoder published a length-padded list), this returns ShortID{} which
// is the zero address. Treating a zero address as a valid signer in a
// quorum count is a silent auth bypass. Always validate non-zero before
// indexing into a keystore, OR use Owner.SyntacticVerify() at the
// boundary (canonical path) — that gate clamps the threshold against
// Len() and rejects empty lists, so a malicious overcount can never
// lead to a "zero co-signer" being accepted.
func (a AddressList) At(i int) ids.ShortID {
	var out ids.ShortID
	if i < 0 || i >= a.list.Len() {
		return out
	}
	// Read 20 bytes via the list's underlying byte indexing.
	obj := a.list.Object(i, ids.ShortIDLen)
	for j := 0; j < ids.ShortIDLen; j++ {
		out[j] = obj.Uint8(j)
	}
	return out
}

// AddressListView reads an AddressList from a parent object's field offset.
// Uses the per-stride clamp introduced in zap v0.7.2: a poisoned length
// field that passes the permissive baseline gets rejected here.
//
// READ-ONLY: each address aliases the underlying ZAP buffer.
func AddressListView(parent zap.Object, fieldOffset int) AddressList {
	return AddressList{list: parent.ListStride(fieldOffset, ids.ShortIDLen)}
}

// WriteAddressList writes an address list at the builder's current position
// and returns (offset, entryCount) suitable for ObjectBuilder.SetList.
func WriteAddressList(b *zap.Builder, addrs []ids.ShortID) (offset, entryCount int) {
	if len(addrs) == 0 {
		return 0, 0
	}
	lb := b.StartList(ids.ShortIDLen)
	for _, a := range addrs {
		lb.AddBytes(a[:])
	}
	off, _ := lb.Finish()
	return off, len(addrs)
}

// ErrOwnerSingleAddrUseStub is returned by NewOwner when the input
// addresses slice has length 1 — callers should use OwnerStub directly
// for the zero-alloc fast path. Multi-address Owner exists only when
// threshold > 1 or multiple co-signers are required.
var ErrOwnerSingleAddrUseStub = errors.New(
	"zap_native: Owner requires len(Addresses) >= 2; use OwnerStub for single-address fast path",
)

// Owner SyntacticVerify error set (LP-023 Red round 4, R4V7 — executor
// side gate). The wire layer accepts any (threshold, locktime, addrlist)
// triple as long as the buffer geometry passes the ListStride clamp; a
// malicious encoder can publish threshold=0 (no-op gate) or threshold >
// len(Addresses) (unsatisfiable gate) or empty Addresses (undefined). The
// executor MUST call SyntacticVerify before trusting the Owner.
var (
	// ErrOwnerThresholdZero is returned when threshold == 0. A zero
	// threshold semantically means "no signature required" which is an
	// authorization bypass. The single-address fast path (OwnerStub) is
	// also gated by this — there is no legitimate threshold=0 owner.
	ErrOwnerThresholdZero = errors.New(
		"zap_native: Owner.Threshold must be > 0; threshold=0 disables authorization",
	)

	// ErrOwnerThresholdExceedsAddrs is returned when threshold >
	// Addresses.Len(). An unsatisfiable gate (DoS — spend can never
	// authorize) and on signed-arithmetic consumers can underflow.
	ErrOwnerThresholdExceedsAddrs = errors.New(
		"zap_native: Owner.Threshold exceeds Addresses.Len() — unsatisfiable signer quorum",
	)

	// ErrOwnerAddrsEmpty is returned when Addresses.Len() == 0. With no
	// signer set the gate is undefined; combined with R4V7's threshold=0
	// case this would silently allow any tx to spend.
	ErrOwnerAddrsEmpty = errors.New(
		"zap_native: Owner.Addresses is empty — signer set undefined",
	)

	// ErrOwnerAddrZero is returned by Owner.SyntacticVerify when one or
	// more addresses in the list is the zero ShortID. This closes R4V3:
	// a malicious wire-encoded list with honest overcount (length field
	// claims M, actual entries N < M) returns zero-padded phantom
	// entries at i >= N for some buffer geometries. Treating those as
	// valid signers is an auth bypass. Fail-closed at the gate.
	ErrOwnerAddrZero = errors.New(
		"zap_native: Owner.Addresses contains the zero ShortID — phantom signer",
	)
)

// Owner is the multi-address output owner. It composes a threshold +
// locktime + a variable AddressList. Stride for the AddressList lives in
// the same buffer's variable section; the parent object's fixed section
// carries (threshold, locktime, AddressList ptr).
//
// Wire layout per Owner header (size 16 bytes; the AddressList itself
// lives in the variable section pointed-to by the list pointer):
//
//	Threshold       uint32 @ 0
//	Locktime        uint64 @ 4
//	AddressList     8B     @ 12   (4-byte relOffset + 4-byte length)
//
// Total header = 20 bytes. The Owner header is embedded INLINE in the
// parent tx's fixed section. Variable-section addresses are appended to
// the parent's variable area by WriteOwner.
//
// Design call: OwnerStub remains the canonical single-address path
// (zero-alloc, 32 bytes total). Owner is used ONLY when len(Addresses) >= 2.
// NewOwner refuses the single-address case (ErrOwnerSingleAddrUseStub) so
// callers must consciously pick the right primitive. This forces:
//
//   - The common case stays in the fast path (OwnerStub, 32B inline).
//   - The multi-sig case carries the multi-address overhead explicitly.
//   - No silent fallback; the type system makes the choice load-bearing.
const (
	OffsetOwnerHeader_Threshold   = 0
	OffsetOwnerHeader_Locktime    = 4
	OffsetOwnerHeader_AddressList = 12
	SizeOwnerHeader               = 20
)

// Owner is the zero-copy accessor over an Owner header embedded in a parent
// tx. The AddressList view spans the parent's variable section.
//
// Owner stores the parent + base offset (not a sub-Object) because zap.Object
// is opaque to in-place sub-views; we read each field via parent.UintX at
// the (baseOffset + fieldOffset) position.
type Owner struct {
	parent     zap.Object
	baseOffset int
}

// Threshold returns the signature-required count.
func (o Owner) Threshold() uint32 {
	return o.parent.Uint32(o.baseOffset + OffsetOwnerHeader_Threshold)
}

// Locktime returns the unix timestamp before which the owner cannot spend.
func (o Owner) Locktime() uint64 {
	return o.parent.Uint64(o.baseOffset + OffsetOwnerHeader_Locktime)
}

// Addresses returns the AddressList view. Each address aliases the parent
// ZAP buffer.
func (o Owner) Addresses() AddressList {
	return AddressListView(o.parent, o.baseOffset+OffsetOwnerHeader_AddressList)
}

// SyntacticVerify enforces R4V7 + R4V3 executor-side semantic gates on
// an Owner header parsed from an untrusted wire buffer. Returns one of:
//
//   - nil — Owner is well-formed: threshold ∈ [1, Addresses.Len()],
//     Addresses.Len() > 0, AND every Address is non-zero.
//   - ErrOwnerAddrsEmpty — Addresses.Len() == 0 (signer set undefined).
//   - ErrOwnerThresholdZero — threshold == 0 (authorization bypass).
//   - ErrOwnerThresholdExceedsAddrs — threshold > Addresses.Len()
//     (unsatisfiable quorum).
//   - ErrOwnerAddrZero — at least one address is the zero ShortID. This
//     closes R4V3: a malicious wire-encoded list with honest overcount
//     returns zero-padded phantom entries at i >= actual_count, which
//     a naive consumer treats as a "zero co-signer". Fail-closed here.
//
// CONTRACT (LP-023 Red round 4 R4V7 + R4V3): every tx executor's Verify()
// entry point MUST call SyntacticVerify on every Owner consumed from a
// wire buffer before treating the Threshold/Addresses pair as a quorum
// gate. The wire layer (ZAP) is permissive by design — semantic gates
// live here in the consumer.
//
// Ordering: addrs-empty → threshold-zero → threshold-exceeds → walk for
// zero-addresses. The walk is O(Len()) but acceptable: typical owners
// have <= 5 addresses; the verify is the only authorization gate.
func (o Owner) SyntacticVerify() error {
	addrs := o.Addresses()
	n := addrs.Len()
	if n == 0 {
		return ErrOwnerAddrsEmpty
	}
	t := o.Threshold()
	if t == 0 {
		return ErrOwnerThresholdZero
	}
	if uint64(t) > uint64(n) {
		return ErrOwnerThresholdExceedsAddrs
	}
	for i := 0; i < n; i++ {
		if addrs.At(i) == (ids.ShortID{}) {
			return ErrOwnerAddrZero
		}
	}
	return nil
}

// OwnerView reads an Owner from a parent object's embedded-header offset.
// The Owner header is INLINE within the parent's fixed section (size
// SizeOwnerHeader = 20 bytes), so this returns a sub-view rather than
// dereferencing a relative pointer.
//
// Use case: a parent tx struct reserves SizeOwnerHeader bytes at a known
// offset for the embedded Owner; this constructor returns the accessor.
func OwnerView(parent zap.Object, fieldOffset int) Owner {
	return Owner{parent: parent, baseOffset: fieldOffset}
}

// SyntacticVerify enforces R4V7 executor-side semantic gates on an
// OwnerStub. The stub by construction carries exactly ONE address, so
// the only valid threshold value is 1. Returns:
//
//   - nil — Threshold == 1 and Address != zero-ShortID.
//   - ErrOwnerThresholdZero — Threshold == 0 (auth bypass).
//   - ErrOwnerThresholdExceedsAddrs — Threshold > 1 (unsatisfiable: only
//     one address present, no quorum > 1 can ever be reached).
//
// The address-zero check is intentionally NOT enforced here: a zero
// ShortID is not a wire-protocol violation, only a usability footgun
// (no key can ever match), so the executor catches it at the spend
// stage if the user fat-fingers it.
//
// CONTRACT (parallel of Owner.SyntacticVerify): every tx executor's
// Verify() entry point that consumes an OwnerStub from a wire buffer
// MUST call SyntacticVerify before treating Threshold as a quorum gate.
func (s OwnerStub) SyntacticVerify() error {
	if s.Threshold == 0 {
		return ErrOwnerThresholdZero
	}
	if s.Threshold > 1 {
		return ErrOwnerThresholdExceedsAddrs
	}
	return nil
}

// OwnerInput is the constructor input for a multi-address Owner.
type OwnerInput struct {
	Threshold uint32
	Locktime  uint64
	Addresses []ids.ShortID
}

// NewOwnerInline writes the Owner's variable-section addresses into the
// builder and returns the (threshold, locktime, addrListOff, addrListLen)
// quad ready for the parent ObjectBuilder to write into the embedded
// Owner header. Returns ErrOwnerSingleAddrUseStub when len(Addresses) < 2.
func NewOwnerInline(b *zap.Builder, in OwnerInput) (threshold uint32, locktime uint64, addrListOff, addrListLen int, err error) {
	if len(in.Addresses) < 2 {
		return 0, 0, 0, 0, ErrOwnerSingleAddrUseStub
	}
	off, count := WriteAddressList(b, in.Addresses)
	return in.Threshold, in.Locktime, off, count, nil
}
