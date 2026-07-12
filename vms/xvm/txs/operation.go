// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"errors"
	"sort"

	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/xvm/fxs"
	"github.com/luxfi/utils"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var (
	ErrNilOperation              = errors.New("nil operation is not valid")
	ErrNilFxOperation            = errors.New("nil fx operation is not valid")
	ErrNotSortedAndUniqueUTXOIDs = errors.New("utxo IDs not sorted and unique")
)

// Operation runs a single fx operation (mint / NFT transfer / property burn)
// over a set of consumed UTXOs of a given asset.
//
// Wire: zap object { Asset@0 (32B), UTXOIDs@32 (36B-stride list), Op@40 (the
// fx operation's own (TypeKind, ShapeKind, ZAP) envelope) }. FxID is recovered
// from the Op envelope's TypeKind, so it is not stored on the wire.
type Operation struct {
	lux.Asset
	UTXOIDs []*lux.UTXOID   `json:"inputIDs"`
	FxID    ids.ID          `json:"fxID"`
	Op      fxs.FxOperation `json:"operation"`
}

const (
	offOpAsset   = 0  // 32B
	offOpUTXOIDs = 32 // list ptr (36B stride)
	offOpFxOp    = 40 // bytes ptr
	sizeOp       = 48
)

func (op *Operation) Verify() error {
	switch {
	case op == nil:
		return ErrNilOperation
	case op.Op == nil:
		return ErrNilFxOperation
	case !utils.IsSortedAndUnique(op.UTXOIDs):
		return ErrNotSortedAndUniqueUTXOIDs
	default:
		return verify.All(&op.Asset, op.Op)
	}
}

// Bytes serializes the Operation to its self-delimiting native-ZAP wire object.
func (op *Operation) Bytes() ([]byte, error) {
	fxOp, err := childBytes(op.Op)
	if err != nil {
		return nil, err
	}
	b := zap.NewBuilder(zap.HeaderSize + sizeOp + len(fxOp) + 256)
	utxoOff, utxoCount := writeUTXOIDs(b, op.UTXOIDs)

	ob := b.StartObject(sizeOp)
	ob.SetBytesFixed(offOpAsset, op.Asset.ID[:])
	ob.SetList(offOpUTXOIDs, utxoOff, utxoCount)
	ob.SetBytes(offOpFxOp, fxOp)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

// parseOperation reconstructs an Operation from its wire object, wrapping the
// fx operation on its (TypeKind, ShapeKind) discriminator.
func parseOperation(buf []byte) (*Operation, error) {
	msg, err := zap.Parse(buf)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	fxOp, err := wrapFxOperation(obj.Bytes(offOpFxOp))
	if err != nil {
		return nil, err
	}
	var asset ids.ID
	copy(asset[:], obj.BytesFixedSlice(offOpAsset, 32))
	return &Operation{
		Asset:   lux.Asset{ID: asset},
		UTXOIDs: readUTXOIDs(obj, offOpUTXOIDs),
		Op:      fxOp,
	}, nil
}

// opBytes is the canonical wire sort key for an Operation.
func opBytes(op *Operation) []byte {
	b, _ := op.Bytes()
	return b
}

func SortOperations(ops []*Operation) {
	sort.Slice(ops, func(i, j int) bool {
		return bytes.Compare(opBytes(ops[i]), opBytes(ops[j])) < 0
	})
}

func IsSortedAndUniqueOperations(ops []*Operation) bool {
	for i := 0; i < len(ops)-1; i++ {
		if bytes.Compare(opBytes(ops[i]), opBytes(ops[i+1])) >= 0 {
			return false
		}
	}
	return true
}

type innerSortOperationsWithSigners struct {
	ops     []*Operation
	signers [][]*secp256k1.PrivateKey
}

func (o *innerSortOperationsWithSigners) Less(i, j int) bool {
	return bytes.Compare(opBytes(o.ops[i]), opBytes(o.ops[j])) < 0
}

func (o *innerSortOperationsWithSigners) Len() int {
	return len(o.ops)
}

func (o *innerSortOperationsWithSigners) Swap(i, j int) {
	o.ops[j], o.ops[i] = o.ops[i], o.ops[j]
	o.signers[j], o.signers[i] = o.signers[i], o.signers[j]
}

func SortOperationsWithSigners(ops []*Operation, signers [][]*secp256k1.PrivateKey) {
	sort.Sort(&innerSortOperationsWithSigners{ops: ops, signers: signers})
}
