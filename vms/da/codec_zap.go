// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package da

// ZAP-native codec for DA blobs and certificates.
//
// Replaces the legacy json/v2 marshaling of DABlob / DACert structs to
// the database. Both shapes carry binary fields (KZG commitments,
// per-chunk proofs, validator signatures) that have no good JSON
// representation — json/v2 base64-encodes them, which inflates storage
// and forces decode-on-read.
//
// Both shapes use a single-byte kind discriminator at envelope offset 0:
//
//   blobKind = 1
//   certKind = 2
//
// Wire shape — DABlob (envelope offsets):
//
//	Kind         u8     @ 0    # = blobKind
//	_pad         u8x7   @ 1
//	Height       u64    @ 8
//	TimestampNs  i64    @ 16
//	ChunkCount   u32    @ 24
//	ChunkSize    u32    @ 28
//	ID           32B    @ 32
//	Submitter    32B    @ 64
//	Commitment   bytes  @ 96    (offset+len, 8 bytes)
//	Data         bytes  @ 104
//	Chunks       bytes  @ 112   (concatenated ChunkEntry records)
//
// Each ChunkEntry record is length-prefixed:
//
//	u32 entryLen
//	u8  entryKind   # = chunkEntryKind
//	u32 Index
//	u32 dataLen
//	N   Data
//	u32 proofLen
//	N   Proof
//	u32 commitmentLen
//	N   Commitment
//
// Wire shape — DACert (envelope offsets):
//
//	Kind         u8     @ 0    # = certKind
//	_pad         u8x3   @ 1
//	Threshold    u32    @ 4
//	TimestampS   i64    @ 8
//	Commitment   bytes  @ 16   (DACommitment payload, ZAP-encoded inner)
//	SignerBitmap bytes  @ 24
//	Signatures   bytes  @ 32   (concatenated SigEntry records)
//
// Each SigEntry: u32 sigLen || sig bytes.
//
// Hard fork: DA state is reset whenever the chain state is reset (devnet
// already did this for LP-023). No dual-read.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

const (
	blobKind uint8 = 1
	certKind uint8 = 2

	// DABlob envelope offsets.
	blobOffKind        = 0
	blobOffHeight      = 8
	blobOffTimestampNs = 16
	blobOffChunkCount  = 24
	blobOffChunkSize   = 28
	blobOffID          = 32
	blobOffSubmitter   = 64
	blobOffCommitment  = 96
	blobOffData        = 104
	blobOffChunks      = 112
	sizeBlobEnvelope   = 120

	// ChunkEntry kind.
	chunkEntryKind uint8 = 1

	// DACert envelope offsets.
	certOffKind         = 0
	certOffThreshold    = 4
	certOffTimestampS   = 8
	certOffCommitment   = 16
	certOffSignerBitmap = 24
	certOffSignatures   = 32
	sizeCertEnvelope    = 40

	// DACommitment ZAP envelope offsets (nested inside DACert).
	commitOffBlobID        = 0
	commitOffHeight        = 32
	commitOffChunkCount    = 40
	commitOffCommitment    = 44
	commitOffDataRoot      = 52
	commitOffErasureRoot   = 60
	commitOffValidatorSigs = 68
	sizeCommitment         = 76
)

// ErrCorruptDABlob / ErrCorruptDACert are returned when a parsed buffer
// is internally inconsistent (wrong kind, payload too short, etc.).
var (
	ErrCorruptDABlob = errors.New("da: corrupt blob")
	ErrCorruptDACert = errors.New("da: corrupt cert")
)

// ---------------- DABlob ----------------

func encodeDABlob(b *DABlob) []byte {
	chunks := encodeChunks(b.Chunks)

	bb := zap.NewBuilder(sizeBlobEnvelope + len(b.Data) + len(b.Commitment) + len(chunks) + 64)
	ob := bb.StartObject(sizeBlobEnvelope)
	ob.SetUint8(blobOffKind, blobKind)
	ob.SetUint64(blobOffHeight, b.Height)
	ob.SetInt64(blobOffTimestampNs, b.Timestamp.UnixNano())
	ob.SetUint32(blobOffChunkCount, b.ChunkCount)
	ob.SetUint32(blobOffChunkSize, b.ChunkSize)
	writeIDInto(ob, blobOffID, b.ID)
	writeIDInto(ob, blobOffSubmitter, b.Submitter)
	ob.SetBytes(blobOffCommitment, b.Commitment)
	ob.SetBytes(blobOffData, b.Data)
	ob.SetBytes(blobOffChunks, chunks)
	ob.FinishAsRoot()
	return bb.Finish()
}

func decodeDABlob(data []byte) (*DABlob, error) {
	msg, err := zap.Parse(data)
	if err != nil {
		return nil, err
	}
	root := msg.Root()
	if root.Uint8(blobOffKind) != blobKind {
		return nil, fmt.Errorf("%w: bad kind", ErrCorruptDABlob)
	}

	blob := &DABlob{
		Height:     root.Uint64(blobOffHeight),
		Timestamp:  time.Unix(0, root.Int64(blobOffTimestampNs)),
		ChunkCount: root.Uint32(blobOffChunkCount),
		ChunkSize:  root.Uint32(blobOffChunkSize),
		Commitment: copyBytes(root.Bytes(blobOffCommitment)),
		Data:       copyBytes(root.Bytes(blobOffData)),
	}
	blob.ID = readID32(root, blobOffID)
	blob.Submitter = readID32(root, blobOffSubmitter)

	chunks, err := decodeChunks(root.Bytes(blobOffChunks))
	if err != nil {
		return nil, err
	}
	blob.Chunks = chunks
	return blob, nil
}

func encodeChunks(chunks []*Chunk) []byte {
	var out []byte
	for _, c := range chunks {
		out = append(out, encodeChunkEntry(c)...)
	}
	return out
}

func encodeChunkEntry(c *Chunk) []byte {
	body := make([]byte, 0, 13+len(c.Data)+len(c.Proof)+len(c.Commitment))
	var u4 [4]byte
	binary.LittleEndian.PutUint32(u4[:], c.Index)
	body = append(body, u4[:]...)
	body = appendVarBytes(body, c.Data)
	body = appendVarBytes(body, c.Proof)
	body = appendVarBytes(body, c.Commitment)

	out := make([]byte, 5+len(body))
	binary.LittleEndian.PutUint32(out[0:4], uint32(1+len(body)))
	out[4] = chunkEntryKind
	copy(out[5:], body)
	return out
}

func decodeChunks(entries []byte) ([]*Chunk, error) {
	var chunks []*Chunk
	for len(entries) > 0 {
		if len(entries) < 5 {
			return nil, fmt.Errorf("%w: chunk header", ErrCorruptDABlob)
		}
		entryLen := binary.LittleEndian.Uint32(entries[0:4])
		if uint32(len(entries)) < 4+entryLen {
			return nil, fmt.Errorf("%w: chunk truncated", ErrCorruptDABlob)
		}
		if entries[4] != chunkEntryKind {
			return nil, fmt.Errorf("%w: chunk kind 0x%02x", ErrCorruptDABlob, entries[4])
		}
		payload := entries[5 : 4+entryLen]
		entries = entries[4+entryLen:]
		c, err := decodeChunkEntry(payload)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, nil
}

func decodeChunkEntry(p []byte) (*Chunk, error) {
	if len(p) < 4 {
		return nil, fmt.Errorf("%w: chunk index", ErrCorruptDABlob)
	}
	c := &Chunk{Index: binary.LittleEndian.Uint32(p[0:4])}
	p = p[4:]
	data, rest, err := readVarBytes(p)
	if err != nil {
		return nil, err
	}
	c.Data = append([]byte(nil), data...)
	proof, rest, err := readVarBytes(rest)
	if err != nil {
		return nil, err
	}
	c.Proof = append([]byte(nil), proof...)
	commit, rest, err := readVarBytes(rest)
	if err != nil {
		return nil, err
	}
	c.Commitment = append([]byte(nil), commit...)
	if len(rest) != 0 {
		return nil, fmt.Errorf("%w: chunk trailing %d bytes", ErrCorruptDABlob, len(rest))
	}
	return c, nil
}

// ---------------- DACert + DACommitment ----------------

func encodeDACert(c *DACert) []byte {
	commitBlob := encodeDACommitment(c.Commitment)
	sigsBlob := encodeSignatures(c.Signatures)

	bb := zap.NewBuilder(sizeCertEnvelope + len(commitBlob) + len(sigsBlob) + len(c.SignerBitmap) + 32)
	ob := bb.StartObject(sizeCertEnvelope)
	ob.SetUint8(certOffKind, certKind)
	ob.SetUint32(certOffThreshold, c.Threshold)
	ob.SetInt64(certOffTimestampS, c.Timestamp)
	ob.SetBytes(certOffCommitment, commitBlob)
	ob.SetBytes(certOffSignerBitmap, c.SignerBitmap)
	ob.SetBytes(certOffSignatures, sigsBlob)
	ob.FinishAsRoot()
	return bb.Finish()
}

func decodeDACert(data []byte) (*DACert, error) {
	msg, err := zap.Parse(data)
	if err != nil {
		return nil, err
	}
	root := msg.Root()
	if root.Uint8(certOffKind) != certKind {
		return nil, fmt.Errorf("%w: bad kind", ErrCorruptDACert)
	}

	c := &DACert{
		Threshold:    root.Uint32(certOffThreshold),
		Timestamp:    root.Int64(certOffTimestampS),
		SignerBitmap: copyBytes(root.Bytes(certOffSignerBitmap)),
	}
	commit, err := decodeDACommitment(root.Bytes(certOffCommitment))
	if err != nil {
		return nil, err
	}
	c.Commitment = commit
	sigs, err := decodeSignatures(root.Bytes(certOffSignatures))
	if err != nil {
		return nil, err
	}
	c.Signatures = sigs
	return c, nil
}

func encodeDACommitment(c *DACommitment) []byte {
	if c == nil {
		return nil
	}
	bb := zap.NewBuilder(sizeCommitment + len(c.Commitment) + len(c.DataRoot) + len(c.ErasureRoot) + len(c.ValidatorSigs) + 32)
	ob := bb.StartObject(sizeCommitment)
	writeIDInto(ob, commitOffBlobID, c.BlobID)
	ob.SetUint64(commitOffHeight, c.Height)
	ob.SetUint32(commitOffChunkCount, c.ChunkCount)
	ob.SetBytes(commitOffCommitment, c.Commitment)
	ob.SetBytes(commitOffDataRoot, c.DataRoot)
	ob.SetBytes(commitOffErasureRoot, c.ErasureRoot)
	ob.SetBytes(commitOffValidatorSigs, c.ValidatorSigs)
	ob.FinishAsRoot()
	return bb.Finish()
}

func decodeDACommitment(data []byte) (*DACommitment, error) {
	if len(data) == 0 {
		return nil, nil
	}
	msg, err := zap.Parse(data)
	if err != nil {
		return nil, err
	}
	root := msg.Root()
	c := &DACommitment{
		Height:        root.Uint64(commitOffHeight),
		ChunkCount:    root.Uint32(commitOffChunkCount),
		Commitment:    copyBytes(root.Bytes(commitOffCommitment)),
		DataRoot:      copyBytes(root.Bytes(commitOffDataRoot)),
		ErasureRoot:   copyBytes(root.Bytes(commitOffErasureRoot)),
		ValidatorSigs: copyBytes(root.Bytes(commitOffValidatorSigs)),
	}
	c.BlobID = readID32(root, commitOffBlobID)
	return c, nil
}

func encodeSignatures(sigs [][]byte) []byte {
	var out []byte
	for _, s := range sigs {
		out = appendVarBytes(out, s)
	}
	return out
}

func decodeSignatures(data []byte) ([][]byte, error) {
	var out [][]byte
	rest := data
	for len(rest) > 0 {
		sig, next, err := readVarBytes(rest)
		if err != nil {
			return nil, err
		}
		out = append(out, append([]byte(nil), sig...))
		rest = next
	}
	return out, nil
}

// ---------------- Helpers ----------------

func writeIDInto(ob *zap.ObjectBuilder, off int, id ids.ID) {
	for i := 0; i < 32; i++ {
		ob.SetUint8(off+i, id[i])
	}
}

func readID32(root zap.Object, off int) ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = root.Uint8(off + i)
	}
	return out
}

func appendVarBytes(buf, data []byte) []byte {
	var u4 [4]byte
	binary.LittleEndian.PutUint32(u4[:], uint32(len(data)))
	buf = append(buf, u4[:]...)
	buf = append(buf, data...)
	return buf
}

func readVarBytes(buf []byte) (data, rest []byte, err error) {
	if len(buf) < 4 {
		return nil, nil, fmt.Errorf("%w: varbytes header", ErrCorruptDABlob)
	}
	n := binary.LittleEndian.Uint32(buf[:4])
	if uint32(len(buf)) < 4+n {
		return nil, nil, fmt.Errorf("%w: varbytes payload %d < %d", ErrCorruptDABlob, len(buf)-4, n)
	}
	return buf[4 : 4+n], buf[4+n:], nil
}

func copyBytes(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return append([]byte(nil), b...)
}
