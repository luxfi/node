// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"fmt"
	"time"

	hash "github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/zap"
)

var _ Block = (*StandardBlock)(nil)

// StandardBlock is the X-chain block. The struct is the source of truth; bytes
// is the cached native-ZAP wire encoding. Txs are self-describing via their own
// tx.Bytes(), so a block stores the per-tx byte lengths as a u32 list plus the
// concatenated tx bytes, and Parse re-splits them through txs.Parse (zero copy).
//
// Object fixed section (all offsets object-relative):
//
//	ParentID  32B @ 0
//	Height    u64 @ 32
//	Time      u64 @ 40
//	Root      32B @ 48   (merkle execution root; ids.Empty pre-activation)
//	TxLengths 8B  @ 80   (u32 list ptr — one entry per tx)
//	TxBlob    8B  @ 88   (bytes ptr — concat of each tx.Bytes())
type StandardBlock struct {
	// parent's ID
	PrntID ids.ID `json:"parentID"`
	// This block's height. The genesis block is at height 0.
	Hght uint64 `json:"height"`
	Time uint64 `json:"time"`
	Root ids.ID `json:"merkleRoot"`
	// List of transactions contained in this block.
	Transactions []*txs.Tx `json:"txs"`

	BlockID ids.ID `json:"id"`
	bytes   []byte
}

const (
	offBlkParent = 0  // 32B
	offBlkHeight = 32 // u64
	offBlkTime   = 40 // u64
	offBlkRoot   = 48 // 32B
	offBlkTxLen  = 80 // list ptr
	offBlkTxBlob = 88 // bytes ptr
	sizeBlk      = 96

	txLenStride = 4 // uint32
)

func (b *StandardBlock) ID() ids.ID {
	return b.BlockID
}

func (b *StandardBlock) Parent() ids.ID {
	return b.PrntID
}

func (b *StandardBlock) Height() uint64 {
	return b.Hght
}

func (b *StandardBlock) Timestamp() time.Time {
	return time.Unix(int64(b.Time), 0)
}

func (b *StandardBlock) MerkleRoot() ids.ID {
	return b.Root
}

func (b *StandardBlock) Txs() []*txs.Tx {
	return b.Transactions
}

func (b *StandardBlock) Bytes() []byte {
	return b.bytes
}

func NewStandardBlock(
	parentID ids.ID,
	height uint64,
	timestamp time.Time,
	txList []*txs.Tx,
) (*StandardBlock, error) {
	return NewStandardBlockWithRoot(parentID, height, timestamp, ids.Empty, txList)
}

// NewStandardBlockWithRoot builds a StandardBlock carrying an explicit merkle
// root and serializes it. NewStandardBlock is the root == ids.Empty special
// case — the historical, pre-activation shape. Above the xvm execution_root
// activation height the builder passes the computed execution_root here so it
// is part of the serialized, hashed block bytes.
func NewStandardBlockWithRoot(
	parentID ids.ID,
	height uint64,
	timestamp time.Time,
	root ids.ID,
	txList []*txs.Tx,
) (*StandardBlock, error) {
	blk := &StandardBlock{
		PrntID:       parentID,
		Hght:         height,
		Time:         uint64(timestamp.Unix()),
		Root:         root,
		Transactions: txList,
	}
	bytes, err := blk.serialize()
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal block: %w", err)
	}
	blk.BlockID = hash.ComputeHash256Array(bytes)
	blk.bytes = bytes
	return blk, nil
}

func (b *StandardBlock) serialize() ([]byte, error) {
	bld := zap.NewBuilder(zap.HeaderSize + sizeBlk + 256)
	lenOff, lenCount, blob, err := writeTxList(bld, b.Transactions)
	if err != nil {
		return nil, err
	}
	ob := bld.StartObject(sizeBlk)
	ob.SetBytesFixed(offBlkParent, b.PrntID[:])
	ob.SetUint64(offBlkHeight, b.Hght)
	ob.SetUint64(offBlkTime, b.Time)
	ob.SetBytesFixed(offBlkRoot, b.Root[:])
	ob.SetList(offBlkTxLen, lenOff, lenCount)
	ob.SetBytes(offBlkTxBlob, blob)
	ob.FinishAsRoot()
	return bld.Finish(), nil
}

// parseStandardBlock decodes a native-ZAP X-chain block, byte-preserving:
// ID = hash(bytes) and bytes is the block's authoritative encoding.
func parseStandardBlock(bytes []byte) (Block, error) {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return nil, fmt.Errorf("couldn't parse block: %w", err)
	}
	obj := msg.Root()
	txList, err := readTxList(obj, offBlkTxLen, offBlkTxBlob)
	if err != nil {
		return nil, err
	}
	var parent, root ids.ID
	copy(parent[:], obj.BytesFixedSlice(offBlkParent, 32))
	copy(root[:], obj.BytesFixedSlice(offBlkRoot, 32))
	return &StandardBlock{
		PrntID:       parent,
		Hght:         obj.Uint64(offBlkHeight),
		Time:         obj.Uint64(offBlkTime),
		Root:         root,
		Transactions: txList,
		BlockID:      hash.ComputeHash256Array(bytes),
		bytes:        bytes,
	}, nil
}

// writeTxList encodes txList as a u32 list of per-tx byte lengths plus a
// concatenated blob of their bytes. AddUint32 counts elements, so Finish()'s
// length is the tx count.
func writeTxList(b *zap.Builder, txList []*txs.Tx) (lenOff, lenCount int, blob []byte, err error) {
	if len(txList) == 0 {
		return 0, 0, nil, nil
	}
	lb := b.StartList(txLenStride)
	for i, tx := range txList {
		if tx == nil {
			return 0, 0, nil, fmt.Errorf("nil tx at index %d", i)
		}
		// Block txs come from the mempool already initialized (parsed from
		// gossip or signed). An empty tx-bytes slot means an uninitialized tx
		// reached block construction — a caller bug, not something to paper
		// over with a hidden Initialize side-effect.
		raw := tx.Bytes()
		if len(raw) == 0 {
			return 0, 0, nil, fmt.Errorf("tx %d has no wire bytes (not Initialized before block build)", i)
		}
		lb.AddUint32(uint32(len(raw)))
		blob = append(blob, raw...)
	}
	lenOff, lenCount = lb.Finish()
	return lenOff, lenCount, blob, nil
}

// readTxList reconstructs the txs from the u32 length list and concatenated
// blob, slicing the blob by each stored length and handing each slice to
// txs.Parse (zero copy).
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
