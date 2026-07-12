// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

// Native ZAP block wire: the struct IS the wire. One zap object per block,
// keyed by a 1-byte blockKind discriminator at object offset 0 — the whole
// dispatch. There is no codec, no version prefix, no slot map. Txs are already
// self-describing via their own tx.Bytes(); a tx-bearing block stores the
// per-tx byte lengths as a u32 list and the concatenated tx bytes as a blob,
// so Parse re-splits and hands each slice to txs.Parse (zero copy).
//
// Object fixed section (all offsets are object-relative):
//
//	blockKind  u8  @ 0     abort=1, commit=2, proposal=3, standard=4
//	ParentID   32B @ 1
//	Height     u64 @ 33
//	Time       u64 @ 41
//	TxLengths  8B  @ 49    u32 list ptr — one entry per decision tx (Standard/Proposal)
//	TxBlob     8B  @ 57    bytes ptr — concat of each decision tx.Bytes() (Standard/Proposal)
//	ProposalTx 8B  @ 65    bytes ptr — the single proposal Tx.Bytes()   (Proposal only)
//
// Sizes: abort/commit = 49, standard = 65, proposal = 73.

import (
	"errors"
	"fmt"
	"time"

	hash "github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/runtime"
	"github.com/luxfi/zap"
)

// blockKind is the 1-byte discriminator at object offset 0 of every P-chain
// block buffer. Parse reads it and returns the typed block. There is no codec.
type blockKind uint8

const (
	blkReserved blockKind = iota
	blkAbort
	blkCommit
	blkProposal
	blkStandard
)

// Fixed wire offsets (object-relative) and object sizes.
const (
	offBlkKind       = 0
	offBlkParent     = 1
	offBlkHeight     = 33
	offBlkTime       = 41
	offBlkTxLengths  = 49
	offBlkTxBlob     = 57
	offBlkProposalTx = 65

	sizeDecided  = 49 // abort / commit (no txs)
	sizeStandard = 65 // + TxLengths + TxBlob
	sizeProposal = 73 // + ProposalTx

	txLenStride = 4 // uint32
)

// commonZapBlock is the embedded base for every block type. It holds the zap
// buffer plus the cached block ID and serves the whole shared surface
// (Bytes/ID/Parent/Height/Timestamp/InitRuntime), so each type file only adds
// its constructor, Txs()/Tx() delta accessors, and Visit.
type commonZapBlock struct {
	msg *zap.Message
	id  ids.ID
}

func (b commonZapBlock) Bytes() []byte  { return b.msg.Bytes() }
func (b commonZapBlock) ID() ids.ID     { return b.id }
func (b commonZapBlock) Parent() ids.ID { return readBlkID(b.msg.Root(), offBlkParent) }
func (b commonZapBlock) Height() uint64 { return b.msg.Root().Uint64(offBlkHeight) }

func (b commonZapBlock) Timestamp() time.Time {
	return time.Unix(int64(b.msg.Root().Uint64(offBlkTime)), 0)
}

// InitRuntime binds the runtime into every embedded tx. It dispatches on the
// wire kind so the base — which owns the layout knowledge — is the one place
// that knows where a block's txs live, without depending on the concrete
// type's Txs() (Go embedding has no virtual dispatch).
func (b commonZapBlock) InitRuntime(rt *runtime.Runtime) {
	obj := b.msg.Root()
	switch blockKind(obj.Uint8(offBlkKind)) {
	case blkStandard, blkProposal:
		if list, err := readTxList(obj, offBlkTxLengths, offBlkTxBlob); err == nil {
			for _, tx := range list {
				tx.Unsigned.InitRuntime(rt)
			}
		}
	}
	if blockKind(obj.Uint8(offBlkKind)) == blkProposal {
		if tx, err := readProposalTx(obj); err == nil && tx != nil {
			tx.Unsigned.InitRuntime(rt)
		}
	}
}

// ErrExtraSpace is returned when a block buffer carries bytes beyond the
// self-delimiting zap message — a malleability vector (ID = hash(bytes) would
// change while the wrapped message is identical). Blocks MUST be canonical.
var ErrExtraSpace = errors.New("block: trailing bytes after zap message")

// setID parses the final bytes into the block's zap buffer and derives
// ID = hash(bytes). Used by both the New*Block constructors (fresh buffer) and
// Parse (wire/disk buffer); the bytes are authoritative and never re-encoded.
//
// Rejects trailing bytes: zap.Parse truncates to the header size field, so a
// buffer with extra tail bytes would wrap the same message but hash to a
// different ID. The block wire is canonical + non-malleable — reject it.
func (b *commonZapBlock) setID(bytes []byte) error {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return err
	}
	if msg.Size() != len(bytes) {
		return ErrExtraSpace
	}
	b.msg = msg
	b.id = hash.ComputeHash256Array(bytes)
	return nil
}

// buildBlock is the one place block fields become bytes. It writes any tx list
// into the builder's variable section BEFORE the object (so the object's list
// pointer is a backward reference, matching the tx package), starts the object
// sized for the kind, and sets the fixed fields.
func buildBlock(k blockKind, parentID ids.ID, height, ts uint64, decisionTxs []*txs.Tx, proposalTx *txs.Tx) ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + 256)

	var lenOff, lenCount int
	var blob []byte
	hasTxList := k == blkStandard || k == blkProposal
	if hasTxList {
		var err error
		lenOff, lenCount, blob, err = writeTxList(b, decisionTxs)
		if err != nil {
			return nil, err
		}
	}

	size := sizeDecided
	switch k {
	case blkStandard:
		size = sizeStandard
	case blkProposal:
		size = sizeProposal
	}

	ob := b.StartObject(size)
	ob.SetUint8(offBlkKind, uint8(k))
	ob.SetBytesFixed(offBlkParent, parentID[:])
	ob.SetUint64(offBlkHeight, height)
	ob.SetUint64(offBlkTime, ts)
	if hasTxList {
		ob.SetList(offBlkTxLengths, lenOff, lenCount)
		ob.SetBytes(offBlkTxBlob, blob)
	}
	if k == blkProposal {
		if proposalTx == nil {
			return nil, fmt.Errorf("proposal block requires a proposal tx")
		}
		ob.SetBytes(offBlkProposalTx, proposalTx.Bytes())
	}
	ob.FinishAsRoot()
	return b.Finish(), nil
}

// writeTxList encodes decisionTxs as a u32 list of per-tx byte lengths plus a
// concatenated blob of their bytes. AddUint32 counts elements, so Finish()'s
// length is the tx count (unlike an AddBytes list, which counts bytes).
func writeTxList(b *zap.Builder, decisionTxs []*txs.Tx) (lenOff, lenCount int, blob []byte, err error) {
	if len(decisionTxs) == 0 {
		return 0, 0, nil, nil
	}
	lb := b.StartList(txLenStride)
	for i, tx := range decisionTxs {
		if tx == nil {
			return 0, 0, nil, fmt.Errorf("nil tx at index %d", i)
		}
		raw := tx.Bytes()
		lb.AddUint32(uint32(len(raw)))
		blob = append(blob, raw...)
	}
	lenOff, lenCount = lb.Finish()
	return lenOff, lenCount, blob, nil
}

// readTxList reconstructs the decision txs from the u32 length list at
// lenPtrOff and the concatenated blob at blobPtrOff, slicing the blob by each
// stored length and handing each slice to txs.Parse (zero copy).
func readTxList(obj zap.Object, lenPtrOff, blobPtrOff int) ([]*txs.Tx, error) {
	lengths := obj.ListStride(lenPtrOff, txLenStride)
	n := lengths.Len()
	if n == 0 {
		return nil, nil
	}
	blob := obj.Bytes(blobPtrOff)
	out := make([]*txs.Tx, n)
	cursor := 0
	for i := 0; i < n; i++ {
		size := int(lengths.Uint32(i))
		if size < 0 || cursor+size > len(blob) {
			return nil, fmt.Errorf("block: tx %d length %d overruns blob (%d)", i, size, len(blob))
		}
		tx, err := txs.Parse(blob[cursor : cursor+size])
		if err != nil {
			return nil, fmt.Errorf("block: parse tx %d: %w", i, err)
		}
		out[i] = tx
		cursor += size
	}
	return out, nil
}

// readProposalTx decodes the single proposal Tx of a ProposalBlock; nil if the
// slot is empty.
func readProposalTx(obj zap.Object) (*txs.Tx, error) {
	raw := obj.Bytes(offBlkProposalTx)
	if len(raw) == 0 {
		return nil, nil
	}
	return txs.Parse(raw)
}

// readBlkID reads a 32-byte id at the given object offset.
func readBlkID(o zap.Object, off int) ids.ID {
	var id ids.ID
	copy(id[:], o.BytesFixedSlice(off, 32))
	return id
}
