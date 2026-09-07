// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

// Native ZAP wire for xsvm blocks: the struct IS the wire. One zap object per
// block; the txs are self-describing via their own tx.Marshal(), so a block
// stores the per-tx byte lengths as a u32 list and the concatenated tx bytes as
// a blob, then Parse re-splits and hands each slice to tx.Parse. There is no
// codec, no version prefix, no slot map.
//
// Object fixed section (offsets object-relative, little-endian):
//
//	ParentID  32B @ 0
//	Timestamp i64 @ 32
//	Height    u64 @ 40
//	TxLengths 8B  @ 48   u32 list ptr — one entry per tx
//	TxBlob    8B  @ 56   bytes ptr — concat of each tx.Marshal()

import (
	"fmt"
	"time"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/example/xsvm/tx"
	"github.com/luxfi/zap"
)

const (
	offBlkParentID  = 0
	offBlkTimestamp = 32
	offBlkHeight    = 40
	offBlkTxLengths = 48
	offBlkTxBlob    = 56
	sizeBlk         = 64

	blkTxLenStride = 4 // uint32
	blkIDLen       = 32
)

// Stateless blocks are blocks as they are marshalled/unmarshalled and sent over
// the p2p network. The stateful blocks which can be executed are built from
// Stateless blocks.
type Stateless struct {
	ParentID  ids.ID   `json:"parentID"`
	Timestamp int64    `json:"timestamp"`
	Height    uint64   `json:"height"`
	Txs       []*tx.Tx `json:"txs"`
}

func (b *Stateless) Time() time.Time {
	return time.Unix(b.Timestamp, 0)
}

// Marshal encodes the block as one native ZAP object.
func (b *Stateless) Marshal() ([]byte, error) {
	builder := zap.NewBuilder(zap.HeaderSize + sizeBlk + 256)

	var (
		txBlob   []byte
		lenOff   int
		lenCount int
	)
	if len(b.Txs) > 0 {
		lb := builder.StartList(blkTxLenStride)
		for i, t := range b.Txs {
			raw, err := t.Marshal()
			if err != nil {
				return nil, fmt.Errorf("xsvm/block: marshal tx %d: %w", i, err)
			}
			lb.AddUint32(uint32(len(raw)))
			txBlob = append(txBlob, raw...)
		}
		lenOff, lenCount = lb.Finish()
	}

	ob := builder.StartObject(sizeBlk)
	ob.SetBytesFixed(offBlkParentID, b.ParentID[:])
	ob.SetInt64(offBlkTimestamp, b.Timestamp)
	ob.SetUint64(offBlkHeight, b.Height)
	ob.SetList(offBlkTxLengths, lenOff, lenCount)
	ob.SetBytes(offBlkTxBlob, txBlob)
	ob.FinishAsRoot()
	return builder.Finish(), nil
}

func (b *Stateless) ID() (ids.ID, error) {
	bytes, err := b.Marshal()
	return hash.ComputeHash256Array(bytes), err
}

func Parse(bytes []byte) (*Stateless, error) {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	blk := &Stateless{
		Timestamp: obj.Int64(offBlkTimestamp),
		Height:    obj.Uint64(offBlkHeight),
	}
	copy(blk.ParentID[:], obj.BytesFixedSlice(offBlkParentID, blkIDLen))

	txs, err := parseTxs(obj)
	if err != nil {
		return nil, err
	}
	blk.Txs = txs
	return blk, nil
}

func parseTxs(obj zap.Object) ([]*tx.Tx, error) {
	lengths := obj.ListStride(offBlkTxLengths, blkTxLenStride)
	n := lengths.Len()
	if n == 0 {
		return nil, nil
	}
	blob := obj.Bytes(offBlkTxBlob)
	out := make([]*tx.Tx, n)
	cursor := 0
	for i := 0; i < n; i++ {
		size := int(lengths.Uint32(i))
		if cursor+size > len(blob) {
			return nil, fmt.Errorf("xsvm/block: tx %d length %d overruns blob (%d)", i, size, len(blob))
		}
		t, err := tx.Parse(blob[cursor : cursor+size])
		if err != nil {
			return nil, fmt.Errorf("xsvm/block: parse tx %d: %w", i, err)
		}
		out[i] = t
		cursor += size
	}
	return out, nil
}
