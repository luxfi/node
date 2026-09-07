// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

// Native ZAP block wire: the struct IS the wire. There is no codec, no version
// prefix, no slot map. A 1-byte blockKind at object offset 0 is the whole
// dispatch — Parse reads it and returns the typed block (signed body vs option).
//
// A signed block is unsigned_buffer ‖ sig_buffer (both self-delimiting zap
// messages), so the unsigned bytes are a genuine byte-prefix of the signed
// bytes and the block ID = hash(unsigned prefix). An unsigned block (no
// certificate, no signature) is just the unsigned buffer with no suffix. An
// option is a single self-delimiting message.
//
// Unsigned-block object (kind = blkSigned), all offsets object-relative, LE:
//
//	kind            u8  @ 0
//	ParentID        32B @ 1
//	Timestamp       i64 @ 33
//	PChainHeight    u64 @ 41
//	Epoch.PChainHt  u64 @ 49
//	Epoch.Number    u64 @ 57
//	Epoch.StartTime i64 @ 65
//	Certificate     8B  @ 73   bytes ptr (opaque DER; part of the ID hash)
//	Block           8B  @ 81   bytes ptr (inner block bytes)
//	DARoot          32B @ 89
//	WitnessRoot     32B @ 121
//	MessagesOutRoot 32B @ 153
//	BlobCount       u32 @ 185
//	                                                          size 189
//
// Sig buffer object: Signature bytes ptr @ 0                 size 8
// Header object:     Chain 32B @0, Parent 32B @32, Body 32B @64  size 96
// Option object (kind = blkOption): kind u8 @0, ParentID 32B @1,
//                                   InnerBytes bytes ptr @33  size 41

import (
	"encoding/binary"
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// blockKind is the 1-byte discriminator at object offset 0 of every parseable
// proposervm block buffer. Parse reads it and returns the typed block.
type blockKind uint8

const (
	blkReserved blockKind = iota
	blkSigned             // statelessBlock (may carry cert + appended signature)
	blkOption             // option block
)

var errUnknownBlockType = errors.New("proposervm block: unknown block kind")

// Unsigned-block object offsets + size.
const (
	offKind      = 0
	offParentID  = 1
	offTimestamp = 33
	offPChainHt  = 41

	offEpochHt    = 49
	offEpochNum   = 57
	offEpochStart = 65

	offCert  = 73
	offBlock = 81

	offDARoot      = 89
	offWitnessRoot = 121
	offMsgOutRoot  = 153
	offBlobCount   = 185

	sizeUnsigned = 189

	idLen = 32
)

// Sig buffer object.
const (
	offSig  = 0
	sizeSig = 8
)

// Header object.
const (
	hdrChain   = 0
	hdrParent  = 32
	hdrBody    = 64
	sizeHeader = 96
)

// Option object.
const (
	offOptParent = 1
	offOptInner  = 33
	sizeOption   = 41
)

// zapLen returns the total length of the leading self-delimiting zap message in
// b (its header size field @[12:16]). This is the split point between the
// unsigned prefix and the signature suffix of a signed block.
func zapLen(b []byte) (int, error) {
	if len(b) < zap.HeaderSize {
		return 0, zap.ErrBufferTooSmall
	}
	n := int(binary.LittleEndian.Uint32(b[12:16]))
	if n < zap.HeaderSize || n > len(b) {
		return 0, zap.ErrBufferTooSmall
	}
	return n, nil
}

// buildUnsignedBuffer writes the unsigned block body as one zap object. cert is
// the opaque DER certificate (nil for an unsigned block). The v1.1 DA fields
// (DARoot/WitnessRoot/MessagesOutRoot/BlobCount) are written as zeros — no
// current constructor populates them, but they stay in the wire so the
// SignedBlock accessor surface round-trips if a future builder sets them.
func buildUnsignedBuffer(parentID ids.ID, timestamp int64, pChainHeight uint64, epoch Epoch, cert, blockBytes []byte) []byte {
	var zero [idLen]byte
	b := zap.NewBuilder(zap.HeaderSize + sizeUnsigned + len(cert) + len(blockBytes) + 64)
	ob := b.StartObject(sizeUnsigned)
	ob.SetUint8(offKind, uint8(blkSigned))
	ob.SetBytesFixed(offParentID, parentID[:])
	ob.SetInt64(offTimestamp, timestamp)
	ob.SetUint64(offPChainHt, pChainHeight)
	ob.SetUint64(offEpochHt, epoch.PChainHeight)
	ob.SetUint64(offEpochNum, epoch.Number)
	ob.SetInt64(offEpochStart, epoch.StartTime)
	ob.SetBytes(offCert, cert)
	ob.SetBytes(offBlock, blockBytes)
	ob.SetBytesFixed(offDARoot, zero[:])
	ob.SetBytesFixed(offWitnessRoot, zero[:])
	ob.SetBytesFixed(offMsgOutRoot, zero[:])
	ob.SetUint32(offBlobCount, 0)
	ob.FinishAsRoot()
	return b.Finish()
}

// buildSigBuffer wraps a proposer signature as a standalone zap message,
// appended after the unsigned prefix of a signed block.
func buildSigBuffer(sig []byte) []byte {
	b := zap.NewBuilder(zap.HeaderSize + sizeSig + len(sig))
	ob := b.StartObject(sizeSig)
	ob.SetBytes(offSig, sig)
	ob.FinishAsRoot()
	return b.Finish()
}

// buildHeaderBuffer encodes the (chain, parent, body) header signed by the
// proposer. Deterministic fixed-shape object — Build and verify both hash it.
func buildHeaderBuffer(chainID, parentID, bodyID ids.ID) []byte {
	b := zap.NewBuilder(zap.HeaderSize + sizeHeader)
	ob := b.StartObject(sizeHeader)
	ob.SetBytesFixed(hdrChain, chainID[:])
	ob.SetBytesFixed(hdrParent, parentID[:])
	ob.SetBytesFixed(hdrBody, bodyID[:])
	ob.FinishAsRoot()
	return b.Finish()
}

// buildOptionBuffer encodes an option block as one zap object.
func buildOptionBuffer(parentID ids.ID, innerBytes []byte) []byte {
	b := zap.NewBuilder(zap.HeaderSize + sizeOption + len(innerBytes) + 32)
	ob := b.StartObject(sizeOption)
	ob.SetUint8(offKind, uint8(blkOption))
	ob.SetBytesFixed(offOptParent, parentID[:])
	ob.SetBytes(offOptInner, innerBytes)
	ob.FinishAsRoot()
	return b.Finish()
}

// read32 reads a 32-byte fixed field at the given object offset.
func read32(o zap.Object, off int) [idLen]byte {
	var r [idLen]byte
	copy(r[:], o.BytesFixedSlice(off, idLen))
	return r
}

// concat returns a ‖ b as a fresh slice.
func concat(a, b []byte) []byte {
	out := make([]byte, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}
