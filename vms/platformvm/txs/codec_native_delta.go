// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Shared delta-field encoders (owner, auth) + the tx types whose delta beyond
// the spending envelope is only scalars / ids / owner / auth / bytes. Delta
// fields occupy the fixed section at offsets >= SpendEnvelopeSize; variable
// delta payloads (owner address lists, auth sig-index lists, message bytes)
// are written to the builder's variable section before StartObject and
// referenced by pointer, exactly as the envelope lists are.

import (
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/fx"
	zn "github.com/luxfi/proto/zap_native"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/zap"
)

// ---- fixed-id helpers ----

func setID(ob *zap.ObjectBuilder, off int, id ids.ID)          { ob.SetBytesFixed(off, id[:]) }
func setNodeID(ob *zap.ObjectBuilder, off int, id ids.NodeID)  { ob.SetBytesFixed(off, id[:]) }

func readID(obj zap.Object, off int) ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = obj.Uint8(off + i)
	}
	return out
}

func readNodeID(obj zap.Object, off int) ids.NodeID {
	var out ids.NodeID
	for i := 0; i < ids.NodeIDLen; i++ {
		out[i] = obj.Uint8(off + i)
	}
	return out
}

// ---- auth (verify.Verifiable == *secp256k1fx.Input) ----

// writeAuthDelta writes an auth credential's SigIndices as a uint32 list and
// returns the (offset, count) pointer for the caller's fixed section.
func writeAuthDelta(b *zap.Builder, auth verify.Verifiable) (off, count int, err error) {
	in, ok := auth.(*secp256k1fx.Input)
	if !ok {
		return 0, 0, fmt.Errorf("zap_native: auth type %T unsupported (want *secp256k1fx.Input)", auth)
	}
	off, count = zn.WriteSigIndicesArray(b, in.SigIndices)
	return off, count, nil
}

// readAuthDelta reconstructs the *secp256k1fx.Input auth from its sig-index
// list pointer at ptrOff.
func readAuthDelta(obj zap.Object, ptrOff int) verify.Verifiable {
	arr := zn.SigIndicesArrayView(obj, ptrOff)
	return &secp256k1fx.Input{SigIndices: sigIndicesAll(arr)}
}

func sigIndicesAll(arr zn.SigIndicesArray) []uint32 {
	n := arr.Len()
	if n == 0 {
		return nil
	}
	return arr.Slice(0, uint32(n))
}

// ---- owner (fx.Owner == *secp256k1fx.OutputOwners) ----

// ownerWire holds the inline scalars + address-list pointer for an fx.Owner.
type ownerWire struct {
	threshold uint32
	locktime  uint64
	addrOff   int
	addrCount int
}

// writeOwnerDelta writes an fx.Owner's address list and returns its inline
// scalars + list pointer.
func writeOwnerDelta(b *zap.Builder, o fx.Owner) (ownerWire, error) {
	oo, ok := o.(*secp256k1fx.OutputOwners)
	if !ok {
		return ownerWire{}, fmt.Errorf("zap_native: owner type %T unsupported (want *secp256k1fx.OutputOwners)", o)
	}
	off, count := zn.WriteOwnerAddrArray(b, oo.Addrs)
	return ownerWire{threshold: oo.Threshold, locktime: oo.Locktime, addrOff: off, addrCount: count}, nil
}

// setOwnerDelta writes an ownerWire into the object at (thresholdOff,
// locktimeOff, addrPtrOff).
func setOwnerDelta(ob *zap.ObjectBuilder, thresholdOff, locktimeOff, addrPtrOff int, ow ownerWire) {
	ob.SetUint32(thresholdOff, ow.threshold)
	ob.SetUint64(locktimeOff, ow.locktime)
	ob.SetList(addrPtrOff, ow.addrOff, ow.addrCount)
}

// readOwnerDelta reconstructs an fx.Owner from its inline scalars + address
// list.
func readOwnerDelta(obj zap.Object, thresholdOff, locktimeOff, addrPtrOff int) fx.Owner {
	arr := zn.OwnerAddrArrayView(obj, addrPtrOff)
	n := arr.Len()
	var addrs []ids.ShortID
	if n > 0 {
		addrs = arr.Slice(0, uint32(n))
	}
	return &secp256k1fx.OutputOwners{
		Locktime:  obj.Uint64(locktimeOff),
		Threshold: obj.Uint32(thresholdOff),
		Addrs:     addrs,
	}
}

// newBuilderFor sizes a builder for a spending tx with the given fixed-section
// size plus its Outs/Ins/Memo variable payloads.
func newBuilderFor(fixedSize int, tx *BaseTx) *zap.Builder {
	capHint := zap.HeaderSize + 64 + fixedSize +
		len(tx.Outs)*zn.SizeTransferableOutputFull +
		len(tx.Ins)*zn.SizeTransferableInputFull + len(tx.Memo)
	return zap.NewBuilder(capHint)
}

// =====================================================================
// IncreaseL1ValidatorBalanceTx: envelope + ValidationID + Balance
// =====================================================================

const (
	OffsetIncreaseL1_ValidationID = SpendEnvelopeSize      // 32B
	OffsetIncreaseL1_Balance      = SpendEnvelopeSize + 32 // u64
	SizeIncreaseL1Tx              = SpendEnvelopeSize + 40
)

func marshalIncreaseL1ValidatorBalanceTx(tx *IncreaseL1ValidatorBalanceTx) ([]byte, error) {
	outs, err := toOutputEntries(tx.Outs)
	if err != nil {
		return nil, err
	}
	ins, err := toInputEntries(tx.Ins)
	if err != nil {
		return nil, err
	}
	b := newBuilderFor(SizeIncreaseL1Tx, &tx.BaseTx)
	so := writeSpendingLists(b, outs, ins)
	ob := b.StartObject(SizeIncreaseL1Tx)
	setSpendingEnvelope(ob, zn.TxKindIncreaseL1ValidatorBalance, tx.NetworkID, tx.BlockchainID, so, tx.Memo)
	setID(ob, OffsetIncreaseL1_ValidationID, tx.ValidationID)
	ob.SetUint64(OffsetIncreaseL1_Balance, tx.Balance)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func unmarshalIncreaseL1ValidatorBalanceTx(buf []byte) (UnsignedTx, error) {
	msg, err := zap.Parse(buf)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	tx := &IncreaseL1ValidatorBalanceTx{
		BaseTx:       BaseTx{BaseTx: readSpending(obj)},
		ValidationID: readID(obj, OffsetIncreaseL1_ValidationID),
		Balance:      obj.Uint64(OffsetIncreaseL1_Balance),
	}
	tx.SetBytes(buf)
	return tx, nil
}

// =====================================================================
// SetL1ValidatorWeightTx: envelope + Message bytes
// =====================================================================

const (
	OffsetSetL1Weight_Message = SpendEnvelopeSize // bytes (8B ptr)
	SizeSetL1WeightTx         = SpendEnvelopeSize + 8
)

func marshalSetL1ValidatorWeightTx(tx *SetL1ValidatorWeightTx) ([]byte, error) {
	outs, err := toOutputEntries(tx.Outs)
	if err != nil {
		return nil, err
	}
	ins, err := toInputEntries(tx.Ins)
	if err != nil {
		return nil, err
	}
	b := newBuilderFor(SizeSetL1WeightTx+len(tx.Message), &tx.BaseTx)
	so := writeSpendingLists(b, outs, ins)
	ob := b.StartObject(SizeSetL1WeightTx)
	setSpendingEnvelope(ob, zn.TxKindSetL1ValidatorWeight, tx.NetworkID, tx.BlockchainID, so, tx.Memo)
	ob.SetBytes(OffsetSetL1Weight_Message, tx.Message)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func unmarshalSetL1ValidatorWeightTx(buf []byte) (UnsignedTx, error) {
	msg, err := zap.Parse(buf)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	tx := &SetL1ValidatorWeightTx{BaseTx: BaseTx{BaseTx: readSpending(obj)}}
	if m := obj.Bytes(OffsetSetL1Weight_Message); len(m) > 0 {
		tx.Message = append([]byte(nil), m...)
	}
	tx.SetBytes(buf)
	return tx, nil
}

// =====================================================================
// DisableL1ValidatorTx: envelope + ValidationID + DisableAuth
// =====================================================================

const (
	OffsetDisableL1_ValidationID = SpendEnvelopeSize      // 32B
	OffsetDisableL1_AuthPtr      = SpendEnvelopeSize + 32 // sig-index list ptr (8B)
	SizeDisableL1Tx              = SpendEnvelopeSize + 40
)

func marshalDisableL1ValidatorTx(tx *DisableL1ValidatorTx) ([]byte, error) {
	outs, err := toOutputEntries(tx.Outs)
	if err != nil {
		return nil, err
	}
	ins, err := toInputEntries(tx.Ins)
	if err != nil {
		return nil, err
	}
	b := newBuilderFor(SizeDisableL1Tx, &tx.BaseTx)
	so := writeSpendingLists(b, outs, ins)
	authOff, authCount, err := writeAuthDelta(b, tx.DisableAuth)
	if err != nil {
		return nil, err
	}
	ob := b.StartObject(SizeDisableL1Tx)
	setSpendingEnvelope(ob, zn.TxKindDisableL1Validator, tx.NetworkID, tx.BlockchainID, so, tx.Memo)
	setID(ob, OffsetDisableL1_ValidationID, tx.ValidationID)
	ob.SetList(OffsetDisableL1_AuthPtr, authOff, authCount)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func unmarshalDisableL1ValidatorTx(buf []byte) (UnsignedTx, error) {
	msg, err := zap.Parse(buf)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	tx := &DisableL1ValidatorTx{
		BaseTx:       BaseTx{BaseTx: readSpending(obj)},
		ValidationID: readID(obj, OffsetDisableL1_ValidationID),
		DisableAuth:  readAuthDelta(obj, OffsetDisableL1_AuthPtr),
	}
	tx.SetBytes(buf)
	return tx, nil
}

// =====================================================================
// RemoveChainValidatorTx: envelope + NodeID + Chain + ChainAuth
// =====================================================================

const (
	OffsetRemoveChain_NodeID  = SpendEnvelopeSize      // 20B
	OffsetRemoveChain_Chain   = SpendEnvelopeSize + 20 // 32B
	OffsetRemoveChain_AuthPtr = SpendEnvelopeSize + 52 // sig-index list ptr (8B)
	SizeRemoveChainTx         = SpendEnvelopeSize + 60
)

func marshalRemoveChainValidatorTx(tx *RemoveChainValidatorTx) ([]byte, error) {
	outs, err := toOutputEntries(tx.Outs)
	if err != nil {
		return nil, err
	}
	ins, err := toInputEntries(tx.Ins)
	if err != nil {
		return nil, err
	}
	b := newBuilderFor(SizeRemoveChainTx, &tx.BaseTx)
	so := writeSpendingLists(b, outs, ins)
	authOff, authCount, err := writeAuthDelta(b, tx.ChainAuth)
	if err != nil {
		return nil, err
	}
	ob := b.StartObject(SizeRemoveChainTx)
	setSpendingEnvelope(ob, zn.TxKindRemoveChainValidator, tx.NetworkID, tx.BlockchainID, so, tx.Memo)
	setNodeID(ob, OffsetRemoveChain_NodeID, tx.NodeID)
	setID(ob, OffsetRemoveChain_Chain, tx.Chain)
	ob.SetList(OffsetRemoveChain_AuthPtr, authOff, authCount)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func unmarshalRemoveChainValidatorTx(buf []byte) (UnsignedTx, error) {
	msg, err := zap.Parse(buf)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	tx := &RemoveChainValidatorTx{
		BaseTx:    BaseTx{BaseTx: readSpending(obj)},
		NodeID:    readNodeID(obj, OffsetRemoveChain_NodeID),
		Chain:     readID(obj, OffsetRemoveChain_Chain),
		ChainAuth: readAuthDelta(obj, OffsetRemoveChain_AuthPtr),
	}
	tx.SetBytes(buf)
	return tx, nil
}

// =====================================================================
// CreateNetworkTx: envelope + Owner
// =====================================================================

const (
	OffsetCreateNetwork_OwnerThreshold = SpendEnvelopeSize      // u32
	OffsetCreateNetwork_OwnerLocktime  = SpendEnvelopeSize + 4  // u64
	OffsetCreateNetwork_OwnerAddrPtr   = SpendEnvelopeSize + 12 // list ptr (8B)
	SizeCreateNetworkTx                = SpendEnvelopeSize + 20
)

func marshalCreateNetworkTx(tx *CreateNetworkTx) ([]byte, error) {
	outs, err := toOutputEntries(tx.Outs)
	if err != nil {
		return nil, err
	}
	ins, err := toInputEntries(tx.Ins)
	if err != nil {
		return nil, err
	}
	b := newBuilderFor(SizeCreateNetworkTx, &tx.BaseTx)
	so := writeSpendingLists(b, outs, ins)
	ow, err := writeOwnerDelta(b, tx.Owner)
	if err != nil {
		return nil, err
	}
	ob := b.StartObject(SizeCreateNetworkTx)
	setSpendingEnvelope(ob, zn.TxKindCreateNetwork, tx.NetworkID, tx.BlockchainID, so, tx.Memo)
	setOwnerDelta(ob, OffsetCreateNetwork_OwnerThreshold, OffsetCreateNetwork_OwnerLocktime, OffsetCreateNetwork_OwnerAddrPtr, ow)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func unmarshalCreateNetworkTx(buf []byte) (UnsignedTx, error) {
	msg, err := zap.Parse(buf)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	tx := &CreateNetworkTx{
		BaseTx: BaseTx{BaseTx: readSpending(obj)},
		Owner:  readOwnerDelta(obj, OffsetCreateNetwork_OwnerThreshold, OffsetCreateNetwork_OwnerLocktime, OffsetCreateNetwork_OwnerAddrPtr),
	}
	tx.SetBytes(buf)
	return tx, nil
}
