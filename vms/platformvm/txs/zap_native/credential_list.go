// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/zap"
)

// SigBlobSize is the canonical secp256k1 signature length (65 bytes:
// 64-byte signature + 1-byte recovery id). Credentials in the v3 schema use
// this fixed stride for the shared signature array. Post-quantum credentials
// (ML-DSA) travel through the legacy codec gate until their dedicated v3
// schema lands.
const SigBlobSize = secp256k1.SignatureLen // 65

// Credential is the primitive entry of a CredentialList. Each entry is a
// fixed-stride record describing a slice into the parent tx's shared
// SignatureArray (which holds SigBlobSize-byte signatures concatenated).
// One credential corresponds to one input (1:1 by index).
//
// Wire layout per entry (stride 16 bytes):
//
//	SigsStart  uint32 @ 0     (index into shared SignatureArray)
//	SigsCount  uint32 @ 4     (number of signatures for this credential)
//	Reserved   8B     @ 8     (reserved-zero; 8-aligned stride)
const (
	OffsetCredential_SigsStart = 0 // uint32
	OffsetCredential_SigsCount = 4 // uint32
	SizeCredential             = 16
)

// Credential is the zero-copy view over one entry in a CredentialList.
type Credential struct {
	obj zap.Object
}

// SigsStart returns the start index into the shared signature array.
func (c Credential) SigsStart() uint32 {
	return c.obj.Uint32(OffsetCredential_SigsStart)
}

// SigsCount returns the number of signatures for this credential.
func (c Credential) SigsCount() uint32 {
	return c.obj.Uint32(OffsetCredential_SigsCount)
}

// CredentialList is the zero-copy view over a list of Credentials.
type CredentialList struct {
	list zap.List
}

// Len returns the credential count.
func (l CredentialList) Len() int { return l.list.Len() }

// IsNull returns true if no list pointer was set.
func (l CredentialList) IsNull() bool { return l.list.IsNull() }

// At returns the i'th Credential.
func (l CredentialList) At(i int) Credential {
	return Credential{obj: l.list.Object(i, SizeCredential)}
}

// CredentialListView reads a CredentialList from a parent object's field
// offset.
func CredentialListView(parent zap.Object, fieldOffset int) CredentialList {
	return CredentialList{list: parent.List(fieldOffset)}
}

// SignatureArray is the zero-copy view over a parent tx's shared signature
// blob array. Each credential's signature slice is
// SignatureArray.Slice(start, count) where each blob is SigBlobSize bytes.
type SignatureArray struct {
	list zap.List
}

// Len returns the total number of signatures across all credentials.
func (s SignatureArray) Len() int { return s.list.Len() }

// IsNull returns true if no array pointer was set.
func (s SignatureArray) IsNull() bool { return s.list.IsNull() }

// At returns the i'th signature as a by-value [SigBlobSize]byte. By-value
// return keeps the accessor zero-allocation (the array is stack-allocated at
// the caller). Returns the zero array for out-of-range i.
func (s SignatureArray) At(i int) [SigBlobSize]byte {
	var out [SigBlobSize]byte
	if i < 0 || i >= s.list.Len() {
		return out
	}
	obj := s.list.Object(i, SigBlobSize)
	for j := 0; j < SigBlobSize; j++ {
		out[j] = obj.Uint8(j)
	}
	return out
}

// CredentialListEntry is the constructor input for a CredentialList.
type CredentialListEntry struct {
	// Sigs is the per-credential signature slice. Each sig MUST be exactly
	// SigBlobSize (65) bytes. The constructor panics on a length violation
	// since wrong-length sigs are a constructor bug (caller-side) not a
	// wire-format malleability surface.
	Sigs [][SigBlobSize]byte
}

// WriteCredentialList writes a credential list and emits its fixed-stride
// entries. Each entry's SigsStart points into the SignatureArray returned by
// the second result; callers MUST write that array via
// WriteSignatureArray and store its (offset, length) in the parent tx's
// sibling SignatureArray field.
func WriteCredentialList(b *zap.Builder, entries []CredentialListEntry) (
	credentialListOffset, credentialListCount int,
	signaturesAll [][SigBlobSize]byte,
) {
	if len(entries) == 0 {
		return 0, 0, nil
	}

	totalSigs := 0
	for _, e := range entries {
		totalSigs += len(e.Sigs)
	}
	signaturesAll = make([][SigBlobSize]byte, 0, totalSigs)

	lb := b.StartList(SizeCredential)
	cursor := uint32(0)
	for _, e := range entries {
		var entry [SizeCredential]byte
		putU32(entry[OffsetCredential_SigsStart:], cursor)
		putU32(entry[OffsetCredential_SigsCount:], uint32(len(e.Sigs)))
		lb.AddBytes(entry[:])

		signaturesAll = append(signaturesAll, e.Sigs...)
		cursor += uint32(len(e.Sigs))
	}
	off, _ := lb.Finish()
	return off, len(entries), signaturesAll
}

// WriteSignatureArray writes the concatenated SigBlobSize-byte signature
// array as a ZAP list and returns (offset, entryCount) for
// ObjectBuilder.SetList. Pair with WriteCredentialList above.
func WriteSignatureArray(b *zap.Builder, sigs [][SigBlobSize]byte) (offset, entryCount int) {
	if len(sigs) == 0 {
		return 0, 0
	}
	lb := b.StartList(SigBlobSize)
	for _, s := range sigs {
		lb.AddBytes(s[:])
	}
	off, _ := lb.Finish()
	return off, len(sigs)
}

// SignatureArrayView reads the shared signature array from a parent object's
// field offset.
func SignatureArrayView(parent zap.Object, fieldOffset int) SignatureArray {
	return SignatureArray{list: parent.List(fieldOffset)}
}
