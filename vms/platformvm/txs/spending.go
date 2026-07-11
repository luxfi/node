// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Shared spending wire: the envelope (NetworkID/BlockchainID/Outs/Ins/Memo)
// every non-proposal P-chain tx carries, plus the fixed-stride Output/Input
// entries and the owner/credential encoders. Built directly on github.com/luxfi/zap
// generic list/object primitives — no codec, no compound wire package. New*Tx
// builds; accessors read.
//
// Envelope fixed section (77 bytes; delta fields of embedding txs start at
// spendSize):
//
//	kind          u8  @ 0
//	NetworkID     u32 @ 1
//	BlockchainID  32B @ 5
//	Outs          8B  @ 37  (list ptr: relOff+count)
//	OwnerAddrs    8B  @ 45  (shared owner-address array ptr)
//	Ins           8B  @ 53  (list ptr)
//	SigIndices    8B  @ 61  (shared input sig-index array ptr)
//	Memo          8B  @ 69  (bytes ptr)

import (
	"encoding/binary"
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/zap"
)

// spendingTx is the embedded base for every non-proposal tx. It holds the zap
// buffer and serves the whole envelope surface (Bytes/NetworkID/Outputs/
// InputIDs/Memo), so each type file only adds its delta accessors + Visit +
// SyntacticVerify.
type spendingTx struct {
	msg *zap.Message
}

func (t spendingTx) Bytes() []byte {
	if t.msg == nil { // uninitialized tx (never Parsed/New*Tx'd) has no wire bytes
		return nil
	}
	return t.msg.Bytes()
}
func (t *spendingTx) SetBytes(b []byte) { t.msg, _ = zap.Parse(b) }
func (t spendingTx) root() zap.Object   { return t.msg.Root() }

func (t spendingTx) NetworkID() uint32          { return t.msg.Root().Uint32(offNetworkID) }
func (t spendingTx) BlockchainID() ids.ID       { return readID(t.msg.Root(), offBlockchainID) }
func (spendingTx) InitRuntime(*runtime.Runtime) {}

func (t spendingTx) Outputs() []*lux.TransferableOutput {
	return readOutputs(t.msg.Root(), offOuts, offOwnerAddrs)
}

func (t spendingTx) Inputs() []*lux.TransferableInput {
	return readInputs(t.msg.Root(), offIns, offSigIndices)
}

func (t spendingTx) Memo() []byte {
	if m := t.msg.Root().Bytes(offMemo); len(m) > 0 {
		return append([]byte(nil), m...)
	}
	return nil
}

func (t spendingTx) InputIDs() set.Set[ids.ID] {
	ins := t.Inputs()
	inputIDs := set.NewSet[ids.ID](len(ins))
	for _, in := range ins {
		inputIDs.Add(in.InputID())
	}
	return inputIDs
}

// baseTx reconstructs the embedded lux.BaseTx (used by SyntacticVerify paths
// that validate the spending envelope as a whole).
func (t spendingTx) baseTx() lux.BaseTx { return readEnvelope(t.msg.Root()) }

const (
	offNetworkID    = 1
	offBlockchainID = 5
	offOuts         = 37
	offOwnerAddrs   = 45
	offIns          = 53
	offSigIndices   = 61
	offMemo         = 69
	spendSize       = 77
)

// output entry (72-byte stride): asset + stake-lock + amount + owner header +
// slice into the shared owner-address array.
const (
	outAssetID   = 0
	outStakeLock = 32
	outAmount    = 40
	outThreshold = 48
	outOwnerLock = 52
	outAddrStart = 60
	outAddrCount = 64
	outStride    = 72
)

// input entry (96-byte stride): utxo id + asset + stake-lock + amount + slice
// into the shared sig-index array.
const (
	inTxID        = 0
	inOutputIndex = 32
	inAssetID     = 36
	inStakeLock   = 68
	inAmount      = 76
	inSigStart    = 84
	inSigCount    = 88
	inStride      = 96
)

const (
	addrStride = 20 // ids.ShortIDLen
	sigStride  = 4  // uint32
	sigLen     = 65 // secp256k1 signature
)

// ---- construction helpers (fields -> buffer, only inside New*Tx) ----

// writeSpending writes the Outs, owner-address array, Ins and sig-index array
// into the builder's variable section (before the object) and returns their
// offsets. Callers set them into the object's fixed section via setEnvelope.
type spendPtrs struct {
	outsOff, outsCount int
	addrOff, addrCount int
	insOff, insCount   int
	sigOff, sigCount   int
}

func writeOutputs(b *zap.Builder, outs []*lux.TransferableOutput) (listOff, listCount, addrOff, addrCount int, err error) {
	if len(outs) == 0 {
		return 0, 0, 0, 0, nil
	}
	var addrs []ids.ShortID
	lb := b.StartList(outStride)
	for i, o := range outs {
		asset, amt, threshold, ownerLock, oaddrs, stakeLock, err := explodeOutput(o)
		if err != nil {
			return 0, 0, 0, 0, fmt.Errorf("output %d: %w", i, err)
		}
		var e [outStride]byte
		copy(e[outAssetID:], asset[:])
		putU64(e[outStakeLock:], stakeLock)
		putU64(e[outAmount:], amt)
		putU32(e[outThreshold:], threshold)
		putU64(e[outOwnerLock:], ownerLock)
		putU32(e[outAddrStart:], uint32(len(addrs)))
		putU32(e[outAddrCount:], uint32(len(oaddrs)))
		lb.AddBytes(e[:])
		addrs = append(addrs, oaddrs...)
	}
	// AddBytes counts BYTES, not elements — use the real element counts.
	listOff, _ = lb.Finish()
	listCount = len(outs)
	if len(addrs) > 0 {
		alb := b.StartList(addrStride)
		for _, a := range addrs {
			alb.AddBytes(a[:])
		}
		addrOff, _ = alb.Finish()
		addrCount = len(addrs)
	}
	return listOff, listCount, addrOff, addrCount, nil
}

func writeInputs(b *zap.Builder, ins []*lux.TransferableInput) (listOff, listCount, sigOff, sigCount int, err error) {
	if len(ins) == 0 {
		return 0, 0, 0, 0, nil
	}
	var sigs []uint32
	lb := b.StartList(inStride)
	for i, in := range ins {
		txID, outIdx, asset, amt, sigIdx, stakeLock, err := explodeInput(in)
		if err != nil {
			return 0, 0, 0, 0, fmt.Errorf("input %d: %w", i, err)
		}
		var e [inStride]byte
		copy(e[inTxID:], txID[:])
		putU32(e[inOutputIndex:], outIdx)
		copy(e[inAssetID:], asset[:])
		putU64(e[inStakeLock:], stakeLock)
		putU64(e[inAmount:], amt)
		putU32(e[inSigStart:], uint32(len(sigs)))
		putU32(e[inSigCount:], uint32(len(sigIdx)))
		lb.AddBytes(e[:])
		sigs = append(sigs, sigIdx...)
	}
	// AddBytes counts BYTES; AddUint32 counts elements — fix the ins list count.
	listOff, _ = lb.Finish()
	listCount = len(ins)
	if len(sigs) > 0 {
		slb := b.StartList(sigStride)
		for _, s := range sigs {
			slb.AddUint32(s)
		}
		sigOff, sigCount = slb.Finish()
	}
	return listOff, listCount, sigOff, sigCount, nil
}

// writeSpending writes the full envelope's variable section (outs+addrs+ins+sigs).
func writeSpending(b *zap.Builder, base *lux.BaseTx) (spendPtrs, error) {
	var p spendPtrs
	var err error
	p.outsOff, p.outsCount, p.addrOff, p.addrCount, err = writeOutputs(b, base.Outs)
	if err != nil {
		return p, err
	}
	p.insOff, p.insCount, p.sigOff, p.sigCount, err = writeInputs(b, base.Ins)
	return p, err
}

// setEnvelope writes the shared envelope fields into an already-started object.
func setEnvelope(ob *zap.ObjectBuilder, k kind, base *lux.BaseTx, p spendPtrs) {
	ob.SetUint8(offKind, uint8(k))
	ob.SetUint32(offNetworkID, base.NetworkID)
	ob.SetBytesFixed(offBlockchainID, base.BlockchainID[:])
	ob.SetList(offOuts, p.outsOff, p.outsCount)
	ob.SetList(offOwnerAddrs, p.addrOff, p.addrCount)
	ob.SetList(offIns, p.insOff, p.insCount)
	ob.SetList(offSigIndices, p.sigOff, p.sigCount)
	ob.SetBytes(offMemo, base.Memo)
}

// ---- accessor helpers (buffer -> values, lazily) ----

// readEnvelope reconstructs a lux.BaseTx from the object's envelope fields.
func readEnvelope(obj zap.Object) lux.BaseTx {
	var blockchainID ids.ID
	copy(blockchainID[:], obj.BytesFixedSlice(offBlockchainID, 32))
	var memo []byte
	if m := obj.Bytes(offMemo); len(m) > 0 {
		memo = append([]byte(nil), m...)
	}
	return lux.BaseTx{
		NetworkID:    obj.Uint32(offNetworkID),
		BlockchainID: blockchainID,
		Outs:         readOutputs(obj, offOuts, offOwnerAddrs),
		Ins:          readInputs(obj, offIns, offSigIndices),
		Memo:         memo,
	}
}

func readOutputs(obj zap.Object, listOff, addrOff int) []*lux.TransferableOutput {
	list := obj.ListStride(listOff, outStride)
	addrs := obj.ListStride(addrOff, addrStride)
	n := list.Len()
	if n == 0 {
		return nil
	}
	outs := make([]*lux.TransferableOutput, n)
	for i := 0; i < n; i++ {
		e := list.Object(i, outStride)
		outs[i] = assembleOutput(
			readID(e, outAssetID),
			e.Uint64(outStakeLock),
			e.Uint64(outAmount),
			e.Uint32(outThreshold),
			e.Uint64(outOwnerLock),
			sliceAddrs(addrs, e.Uint32(outAddrStart), e.Uint32(outAddrCount)),
		)
	}
	return outs
}

func readInputs(obj zap.Object, listOff, sigOff int) []*lux.TransferableInput {
	list := obj.ListStride(listOff, inStride)
	sigs := obj.ListStride(sigOff, sigStride)
	n := list.Len()
	if n == 0 {
		return nil
	}
	ins := make([]*lux.TransferableInput, n)
	for i := 0; i < n; i++ {
		e := list.Object(i, inStride)
		ins[i] = assembleInput(
			readID(e, inTxID),
			e.Uint32(inOutputIndex),
			readID(e, inAssetID),
			e.Uint64(inStakeLock),
			e.Uint64(inAmount),
			sliceSigs(sigs, e.Uint32(inSigStart), e.Uint32(inSigCount)),
		)
	}
	return ins
}

// ---- polymorphism: TransferOutput/LockOut, TransferInput/LockIn ----

func explodeOutput(o *lux.TransferableOutput) (asset ids.ID, amt uint64, threshold uint32, ownerLock uint64, addrs []ids.ShortID, stakeLock uint64, err error) {
	inner := o.Out
	if lo, ok := inner.(*stakeable.LockOut); ok {
		stakeLock = lo.Locktime
		inner = lo.TransferableOut
	}
	to, ok := inner.(*secp256k1fx.TransferOutput)
	if !ok {
		return asset, 0, 0, 0, nil, 0, fmt.Errorf("unsupported FxOutput %T", o.Out)
	}
	return o.Asset.ID, to.Amt, to.Threshold, to.Locktime, to.Addrs, stakeLock, nil
}

func assembleOutput(asset ids.ID, stakeLock, amt uint64, threshold uint32, ownerLock uint64, addrs []ids.ShortID) *lux.TransferableOutput {
	to := &secp256k1fx.TransferOutput{
		Amt:          amt,
		OutputOwners: secp256k1fx.OutputOwners{Locktime: ownerLock, Threshold: threshold, Addrs: addrs},
	}
	var out lux.TransferableOut = to
	if stakeLock != 0 {
		out = &stakeable.LockOut{Locktime: stakeLock, TransferableOut: to}
	}
	return &lux.TransferableOutput{Asset: lux.Asset{ID: asset}, Out: out}
}

func explodeInput(in *lux.TransferableInput) (txID ids.ID, outIdx uint32, asset ids.ID, amt uint64, sigIdx []uint32, stakeLock uint64, err error) {
	inner := in.In
	if li, ok := inner.(*stakeable.LockIn); ok {
		stakeLock = li.Locktime
		inner = li.TransferableIn
	}
	ti, ok := inner.(*secp256k1fx.TransferInput)
	if !ok {
		return txID, 0, asset, 0, nil, 0, fmt.Errorf("unsupported FxInput %T", in.In)
	}
	return in.UTXOID.TxID, in.UTXOID.OutputIndex, in.Asset.ID, ti.Amt, ti.SigIndices, stakeLock, nil
}

func assembleInput(txID ids.ID, outIdx uint32, asset ids.ID, stakeLock, amt uint64, sigIdx []uint32) *lux.TransferableInput {
	ti := &secp256k1fx.TransferInput{Amt: amt, Input: secp256k1fx.Input{SigIndices: sigIdx}}
	var in lux.TransferableIn = ti
	if stakeLock != 0 {
		in = &stakeable.LockIn{Locktime: stakeLock, TransferableIn: ti}
	}
	return &lux.TransferableInput{
		UTXOID: lux.UTXOID{TxID: txID, OutputIndex: outIdx},
		Asset:  lux.Asset{ID: asset},
		In:     in,
	}
}

// ---- shared array slicing (bounds-clamped) ----

func sliceAddrs(arr zap.List, start, count uint32) []ids.ShortID {
	total := uint32(arr.Len())
	if count == 0 || start > total || count > total-start {
		return nil
	}
	out := make([]ids.ShortID, count)
	for i := uint32(0); i < count; i++ {
		o := arr.Object(int(start+i), addrStride)
		for j := 0; j < addrStride; j++ {
			out[i][j] = o.Uint8(j)
		}
	}
	return out
}

func sliceSigs(arr zap.List, start, count uint32) []uint32 {
	total := uint32(arr.Len())
	if count == 0 || start > total || count > total-start {
		return nil
	}
	out := make([]uint32, count)
	for i := uint32(0); i < count; i++ {
		out[i] = arr.Uint32(int(start + i))
	}
	return out
}

// readID reads a 32-byte id at the given offset of an object.
func readID(o zap.Object, off int) ids.ID {
	var id ids.ID
	copy(id[:], o.BytesFixedSlice(off, 32))
	return id
}

// putU32/putU64 are little-endian writers for stride scratch buffers.
func putU32(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }
func putU64(b []byte, v uint64) { binary.LittleEndian.PutUint64(b, v) }
