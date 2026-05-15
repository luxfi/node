// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"errors"
	"sort"

	"github.com/luxfi/codec"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/node/vms/components/verify"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utils"
	"github.com/luxfi/utxo/secp256k1fx"
)

// Operation expresses an action against a previously-minted asset's UTXOs
// (transfer, mint, burn, NFT-style state op). PlatformVM exposes only the
// secp256k1fx Fx so [Op] is a secp256k1fx-compatible verifiable input.
type Operation struct {
	lux.Asset `serialize:"true"`

	// UTXOIDs of UTXOs this operation consumes.
	UTXOIDs []*lux.UTXOID `serialize:"true" json:"inputIDs"`

	// Op is the action to perform. On P-Chain this is always a
	// secp256k1fx-flavoured operation; the codec keeps the door open for
	// additional Fx flavours by storing a verify.Verifiable.
	Op verify.Verifiable `serialize:"true" json:"operation"`
}

var (
	ErrNilOperation              = errors.New("nil operation is not valid")
	ErrNilFxOperation            = errors.New("nil fx operation is not valid")
	ErrNotSortedAndUniqueUTXOIDs = errors.New("utxo ids on operation are not sorted and unique")
	// ErrUnsupportedOpType pins [Op] to the single Fx PlatformVM ships
	// (secp256k1fx). Anything else — propertyfx, nftfx, a future Fx an
	// attacker tries to slip in — is rejected at syntactic verification
	// time before any state-modifying executor sees it.
	ErrUnsupportedOpType = errors.New("operation op type is not supported on PlatformVM (secp256k1fx only)")
)

// Verify returns nil iff this Operation is well formed.
func (op *Operation) Verify() error {
	switch {
	case op == nil:
		return ErrNilOperation
	case op.Op == nil:
		return ErrNilFxOperation
	case !utils.IsSortedAndUnique(op.UTXOIDs):
		return ErrNotSortedAndUniqueUTXOIDs
	}
	if _, ok := op.Op.(*secp256k1fx.MintOperation); !ok {
		return ErrUnsupportedOpType
	}
	return verify.All(&op.Asset, op.Op)
}

// secp256k1fx is the only Fx on PlatformVM. The compile-time check guards
// against drift if a developer tries to wire a non-secp256k1fx operation in.
var _ = secp256k1fx.ID

// SortOperations sorts the given operations by their codec-marshalled bytes.
type operationAndCodec struct {
	op    *Operation
	codec codec.Manager
}

func (o *operationAndCodec) Compare(other *operationAndCodec) int {
	oBytes, err := o.codec.Marshal(CodecVersion, o.op)
	if err != nil {
		return 0
	}
	otherBytes, err := o.codec.Marshal(CodecVersion, other.op)
	if err != nil {
		return 0
	}
	return bytes.Compare(oBytes, otherBytes)
}

func SortOperations(ops []*Operation, c codec.Manager) {
	wrapped := make([]*operationAndCodec, len(ops))
	for i, op := range ops {
		wrapped[i] = &operationAndCodec{op: op, codec: c}
	}
	utils.Sort(wrapped)
	for i, w := range wrapped {
		ops[i] = w.op
	}
}

// IsSortedAndUniqueOperations reports whether [ops] is sorted by codec bytes
// and contains no duplicates.
func IsSortedAndUniqueOperations(ops []*Operation, c codec.Manager) bool {
	wrapped := make([]*operationAndCodec, len(ops))
	for i, op := range ops {
		wrapped[i] = &operationAndCodec{op: op, codec: c}
	}
	return utils.IsSortedAndUnique(wrapped)
}

type innerSortOperationsWithSigners struct {
	ops     []*Operation
	signers [][]*secp256k1.PrivateKey
	codec   codec.Manager
}

func (s *innerSortOperationsWithSigners) Less(i, j int) bool {
	iBytes, err := s.codec.Marshal(CodecVersion, s.ops[i])
	if err != nil {
		return false
	}
	jBytes, err := s.codec.Marshal(CodecVersion, s.ops[j])
	if err != nil {
		return false
	}
	return bytes.Compare(iBytes, jBytes) == -1
}

func (s *innerSortOperationsWithSigners) Len() int { return len(s.ops) }
func (s *innerSortOperationsWithSigners) Swap(i, j int) {
	s.ops[j], s.ops[i] = s.ops[i], s.ops[j]
	s.signers[j], s.signers[i] = s.signers[i], s.signers[j]
}

func SortOperationsWithSigners(ops []*Operation, signers [][]*secp256k1.PrivateKey, c codec.Manager) {
	sort.Sort(&innerSortOperationsWithSigners{ops: ops, signers: signers, codec: c})
}
