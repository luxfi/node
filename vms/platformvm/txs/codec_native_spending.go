// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Native-ZAP spending envelope + component converters shared by every P-chain
// tx type that embeds lux.BaseTx (Outs/Ins/Memo). The envelope is a native
// ZAP object; the polymorphic FxOutput/FxInput/credential types are flattened
// into the fixed-stride list primitives in proto/zap_native
// (TransferableOutputFull/InputFull, CredentialList) — no reflection.
//
// Envelope fixed-section layout (77 bytes). Delta fields of embedding tx
// types begin at SpendEnvelopeSize. Credentials are NOT in the unsigned
// object — they travel in the separate creds buffer appended by the signed
// envelope (unsigned ‖ creds), preserving the unsigned-is-prefix invariant.
//
//	TxKind         uint8  @ 0
//	NetworkID      uint32 @ 1
//	BlockchainID   32B    @ 5
//	OutsList       8B     @ 37  (TransferableOutputFull list)
//	OwnerAddrArray 8B     @ 45  (shared multi-address owner array)
//	InsList        8B     @ 53  (TransferableInputFull list)
//	SigIndicesArr  8B     @ 61  (shared input sig-index array)
//	Memo           8B     @ 69  (relOffset + length)

import (
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	zn "github.com/luxfi/proto/zap_native"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/zap"
)

const (
	OffsetSpend_NetworkID     = 1
	OffsetSpend_BlockchainID  = 5
	OffsetSpend_OutsList      = 37
	OffsetSpend_OwnerAddrArr  = 45
	OffsetSpend_InsList       = 53
	OffsetSpend_SigIndicesArr = 61
	OffsetSpend_Memo          = 69
	SpendEnvelopeSize         = 77
)

// spendingOffsets captures the variable-section list positions produced by
// writeSpendingLists, ready to be written into the object's fixed section.
type spendingOffsets struct {
	outsOff, outsCount       int
	addrOff, addrCount       int
	insOff, insCount         int
	sigIdxOff, sigIdxCount   int
}

// writeSpendingLists writes the Outs, OwnerAddr, Ins and SigIndices lists into
// the builder's variable section (BEFORE the parent object's fixed section)
// and returns their offsets. Must be called before StartObject.
func writeSpendingLists(b *zap.Builder, outs []zn.OutputFullListEntry, ins []zn.InputFullListEntry) spendingOffsets {
	outsOff, outsCount, ownerAddrs := zn.WriteOutputFullList(b, outs)
	addrOff, addrCount := zn.WriteOwnerAddrArray(b, ownerAddrs)
	insOff, insCount, sigIndices := zn.WriteInputFullList(b, ins)
	sigIdxOff, sigIdxCount := zn.WriteSigIndicesArray(b, sigIndices)
	return spendingOffsets{
		outsOff: outsOff, outsCount: outsCount,
		addrOff: addrOff, addrCount: addrCount,
		insOff: insOff, insCount: insCount,
		sigIdxOff: sigIdxOff, sigIdxCount: sigIdxCount,
	}
}

// setSpendingEnvelope writes the shared envelope fields (kind, ids, list
// pointers, memo) into an already-started object. Delta fields are written by
// the caller at offsets >= SpendEnvelopeSize.
func setSpendingEnvelope(ob *zap.ObjectBuilder, kind zn.TxKind, networkID uint32, blockchainID ids.ID, so spendingOffsets, memo []byte) {
	ob.SetUint8(zn.OffsetTxKind, uint8(kind))
	ob.SetUint32(OffsetSpend_NetworkID, networkID)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetSpend_BlockchainID+i, blockchainID[i])
	}
	ob.SetList(OffsetSpend_OutsList, so.outsOff, so.outsCount)
	ob.SetList(OffsetSpend_OwnerAddrArr, so.addrOff, so.addrCount)
	ob.SetList(OffsetSpend_InsList, so.insOff, so.insCount)
	ob.SetList(OffsetSpend_SigIndicesArr, so.sigIdxOff, so.sigIdxCount)
	ob.SetBytes(OffsetSpend_Memo, memo)
}

// readSpending reads the shared envelope fields back into a lux.BaseTx.
func readSpending(obj zap.Object) lux.BaseTx {
	var blockchainID ids.ID
	for i := 0; i < 32; i++ {
		blockchainID[i] = obj.Uint8(OffsetSpend_BlockchainID + i)
	}
	outsList := zn.OutputFullListView(obj, OffsetSpend_OutsList)
	addrArr := zn.OwnerAddrArrayView(obj, OffsetSpend_OwnerAddrArr)
	insList := zn.InputFullListView(obj, OffsetSpend_InsList)
	sigIdxArr := zn.SigIndicesArrayView(obj, OffsetSpend_SigIndicesArr)

	var memo []byte
	if m := obj.Bytes(OffsetSpend_Memo); len(m) > 0 {
		memo = append([]byte(nil), m...)
	}

	return lux.BaseTx{
		NetworkID:    obj.Uint32(OffsetSpend_NetworkID),
		BlockchainID: blockchainID,
		Outs:         fromOutputEntries(outsList, addrArr),
		Ins:          fromInputEntries(insList, sigIdxArr),
		Memo:         memo,
	}
}

// ---- component converters: txs struct graph <-> zap_native entries ----

// toOutputEntries flattens []*lux.TransferableOutput (whose Out is either
// *secp256k1fx.TransferOutput or *stakeable.LockOut wrapping one) into the
// fixed-stride entry form.
func toOutputEntries(outs []*lux.TransferableOutput) ([]zn.OutputFullListEntry, error) {
	entries := make([]zn.OutputFullListEntry, len(outs))
	for i, o := range outs {
		var stakeLocktime uint64
		inner := o.Out
		if lo, ok := inner.(*stakeable.LockOut); ok {
			stakeLocktime = lo.Locktime
			inner = lo.TransferableOut
		}
		to, ok := inner.(*secp256k1fx.TransferOutput)
		if !ok {
			return nil, fmt.Errorf("zap_native: output %d has unsupported FxOutput %T", i, o.Out)
		}
		entries[i] = zn.OutputFullListEntry{
			AssetID:        o.Asset.ID,
			StakeLocktime:  stakeLocktime,
			Amount:         to.Amt,
			OwnerThreshold: to.Threshold,
			OwnerLocktime:  to.Locktime,
			Addrs:          to.Addrs,
		}
	}
	return entries, nil
}

func fromOutputEntries(list zn.OutputFullList, addrs zn.OwnerAddrArray) []*lux.TransferableOutput {
	n := list.Len()
	outs := make([]*lux.TransferableOutput, n)
	for i := 0; i < n; i++ {
		e := list.At(i)
		to := &secp256k1fx.TransferOutput{
			Amt: e.Amount(),
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  e.OwnerLocktime(),
				Threshold: e.OwnerThreshold(),
				Addrs:     e.OwnerAddrs(addrs),
			},
		}
		var out lux.TransferableOut = to
		if e.IsStakeableLocked() {
			out = &stakeable.LockOut{Locktime: e.StakeLocktime(), TransferableOut: to}
		}
		outs[i] = &lux.TransferableOutput{Asset: lux.Asset{ID: e.AssetID()}, Out: out}
	}
	return outs
}

func toInputEntries(ins []*lux.TransferableInput) ([]zn.InputFullListEntry, error) {
	entries := make([]zn.InputFullListEntry, len(ins))
	for i, in := range ins {
		var stakeLocktime uint64
		inner := in.In
		if li, ok := inner.(*stakeable.LockIn); ok {
			stakeLocktime = li.Locktime
			inner = li.TransferableIn
		}
		ti, ok := inner.(*secp256k1fx.TransferInput)
		if !ok {
			return nil, fmt.Errorf("zap_native: input %d has unsupported FxInput %T", i, in.In)
		}
		entries[i] = zn.InputFullListEntry{
			TxID:          in.UTXOID.TxID,
			OutputIndex:   in.UTXOID.OutputIndex,
			AssetID:       in.Asset.ID,
			StakeLocktime: stakeLocktime,
			Amount:        ti.Amt,
			SigIndices:    ti.SigIndices,
		}
	}
	return entries, nil
}

func fromInputEntries(list zn.InputFullList, sigArr zn.SigIndicesArray) []*lux.TransferableInput {
	n := list.Len()
	ins := make([]*lux.TransferableInput, n)
	for i := 0; i < n; i++ {
		e := list.At(i)
		ti := &secp256k1fx.TransferInput{
			Amt:   e.Amount(),
			Input: secp256k1fx.Input{SigIndices: e.SigIndices(sigArr)},
		}
		var in lux.TransferableIn = ti
		if e.IsStakeableLocked() {
			in = &stakeable.LockIn{Locktime: e.StakeLocktime(), TransferableIn: ti}
		}
		ins[i] = &lux.TransferableInput{
			UTXOID: lux.UTXOID{TxID: e.TxID(), OutputIndex: e.OutputIndex()},
			Asset:  lux.Asset{ID: e.AssetID()},
			In:     in,
		}
	}
	return ins
}

// toCredEntries / fromCredEntries encode the signed-tx credential list into a
// standalone creds buffer (see marshalCredsNative). Each verify.Verifiable is
// a *secp256k1fx.Credential carrying [][65]byte signatures.
func toCredEntries(creds []verify.Verifiable) ([]zn.CredentialListEntry, error) {
	entries := make([]zn.CredentialListEntry, len(creds))
	for i, c := range creds {
		cred, ok := c.(*secp256k1fx.Credential)
		if !ok {
			return nil, fmt.Errorf("zap_native: credential %d has unsupported type %T", i, c)
		}
		entries[i] = zn.CredentialListEntry{Sigs: cred.Sigs}
	}
	return entries, nil
}

func fromCredEntries(list zn.CredentialList, sigArr zn.SignatureArray) []verify.Verifiable {
	n := list.Len()
	creds := make([]verify.Verifiable, n)
	for i := 0; i < n; i++ {
		c := list.At(i)
		creds[i] = &secp256k1fx.Credential{Sigs: sigArr.Slice(c.SigsStart(), c.SigsCount())}
	}
	return creds
}
