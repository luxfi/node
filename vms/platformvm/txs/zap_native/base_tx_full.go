// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// BaseTxFull is the full P-chain spending envelope: NetworkID + BlockchainID
// + Outs + Ins + Credentials + Memo. Higher-level tx types (Import/Export/
// AddPermissionlessValidator/CreateChain) embed this. The minimal
// metadata-only BaseTx (TxKindBase) remains for places that need only
// {NetworkID, BlockchainID, Memo} without spending state.
//
// Fixed-section layout (size 73 bytes; uint64 reads alignment-tolerant):
//
//	TxKind              uint8  @ 0
//	NetworkID           uint32 @ 1
//	BlockchainID        32B    @ 5
//	OutsListRel         uint32 @ 37   (list pointer; (offset,count))
//	OutsListLen         uint32 @ 41
//	InsListRel          uint32 @ 45
//	InsListLen          uint32 @ 49
//	CredsListRel        uint32 @ 53
//	CredsListLen        uint32 @ 57
//	SigIndicesArrRel    uint32 @ 61   (shared uint32 array; InputList slices here)
//	SigIndicesArrLen    uint32 @ 65
//	SigArrRel           uint32 @ 69   (shared signature blob array; CredentialList slices)
//	SigArrLen           uint32 @ 73
//	Memo                bytes  @ 77   (relOffset + length, 8 bytes)
//	(fixed-section size 85; the 4 list-pointer pairs are 8 bytes each,
//	 stored as 4-byte rel + 4-byte length consistent with zap.Object.List)
//
// Layout summary: header + 5 lists/arrays (each is a 4-byte relOffset and a
// 4-byte length, 40 bytes total for the 5 pointers) + Memo (8 bytes).
//
// Total fixed section = 1 (TxKind) + 4 (NetworkID) + 32 (BlockchainID) +
// 5*8 (4 lists + 1 array pair) + 8 (Memo) = 85 bytes. But the 5 list+arr
// pointers are split into 5×(4+4) = 40 bytes, beginning at offset 37.
const (
	OffsetBaseTxFull_NetworkID     = 1
	OffsetBaseTxFull_BlockchainID  = 5
	OffsetBaseTxFull_OutsList      = 37 // list pointer (8 bytes)
	OffsetBaseTxFull_InsList       = 45 // list pointer (8 bytes)
	OffsetBaseTxFull_CredsList     = 53 // list pointer (8 bytes)
	OffsetBaseTxFull_SigIndicesArr = 61 // list pointer (8 bytes)
	OffsetBaseTxFull_SigArr        = 69 // list pointer (8 bytes)
	OffsetBaseTxFull_Memo          = 77 // bytes (8 bytes)
	SizeBaseTxFull                 = 85
)

// BaseTxFull is a zero-copy typed accessor over a ZAP buffer carrying a v3
// full P-chain spending tx.
type BaseTxFull struct {
	msg *zap.Message
	obj zap.Object
}

// NetworkID returns the network ID the tx targets.
func (t BaseTxFull) NetworkID() uint32 {
	return t.obj.Uint32(OffsetBaseTxFull_NetworkID)
}

// BlockchainID returns the chain ID the tx executes against.
func (t BaseTxFull) BlockchainID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetBaseTxFull_BlockchainID + i)
	}
	return out
}

// Outs returns the OutputList view.
func (t BaseTxFull) Outs() OutputList {
	return OutputListView(t.obj, OffsetBaseTxFull_OutsList)
}

// Ins returns the InputList view.
func (t BaseTxFull) Ins() InputList {
	return InputListView(t.obj, OffsetBaseTxFull_InsList)
}

// Credentials returns the CredentialList view.
func (t BaseTxFull) Credentials() CredentialList {
	return CredentialListView(t.obj, OffsetBaseTxFull_CredsList)
}

// SigIndicesArray returns the shared uint32 sig-index array view that the
// InputList entries slice into.
func (t BaseTxFull) SigIndicesArray() SigIndicesArray {
	return SigIndicesArrayView(t.obj, OffsetBaseTxFull_SigIndicesArr)
}

// SignatureArray returns the shared signature blob array view that the
// CredentialList entries slice into.
func (t BaseTxFull) SignatureArray() SignatureArray {
	return SignatureArrayView(t.obj, OffsetBaseTxFull_SigArr)
}

// Memo returns the memo bytes.
//
// READ-ONLY: aliases the underlying ZAP buffer. Mutation corrupts the
// parsed tx and breaks TxID = hash(buffer). Use append([]byte(nil), m...)
// to take ownership.
func (t BaseTxFull) Memo() []byte {
	return t.obj.Bytes(OffsetBaseTxFull_Memo)
}

func (t BaseTxFull) Bytes() []byte { return t.msg.Bytes() }
func (t BaseTxFull) IsZero() bool  { return t.msg == nil }

// WrapBaseTxFull parses a ZAP buffer into a typed BaseTxFull accessor.
//
// Returns ErrWrongTxKind if the discriminator does not match TxKindBaseFull.
func WrapBaseTxFull(b []byte) (BaseTxFull, error) {
	msg, obj, err := parseAndCheckKind(b, TxKindBaseFull)
	if err != nil {
		return BaseTxFull{}, err
	}
	return BaseTxFull{msg: msg, obj: obj}, nil
}

// BaseTxFullInput is the constructor input for NewBaseTxFull. The lists are
// converted to ZAP wire form inside the builder; no separate writer call
// needed by the caller.
type BaseTxFullInput struct {
	NetworkID    uint32
	BlockchainID ids.ID
	Outs         []OutputListEntry
	Ins          []InputListEntry
	Credentials  []CredentialListEntry
	Memo         []byte
}

// NewBaseTxFull builds a v3 BaseTxFull into a fresh ZAP buffer. Outs/Ins/
// Credentials may each be empty (the corresponding list pointer is
// (0, 0)).
func NewBaseTxFull(in BaseTxFullInput) BaseTxFull {
	// Capacity estimate: header + fixed section + list payloads + memo.
	cap := zap.HeaderSize + 16 + SizeBaseTxFull
	cap += len(in.Outs) * SizeTransferableOutput
	cap += len(in.Ins) * SizeTransferableInput
	cap += len(in.Credentials) * SizeCredential
	cap += len(in.Memo)
	b := zap.NewBuilder(cap)

	// Write all lists FIRST so they're positioned in the variable section
	// before the parent object's fixed section. The parent's list pointers
	// will then be backward (negative) — ZAP's signed-int32 list/Object
	// decoding accepts that, post-F1.
	outsOff, outsCount := WriteOutputList(b, in.Outs)
	insOff, insCount, sigIndices := WriteInputList(b, in.Ins)
	credsOff, credsCount, sigBlobs := WriteCredentialList(b, in.Credentials)
	sigIdxArrOff, sigIdxArrCount := WriteSigIndicesArray(b, sigIndices)
	sigArrOff, sigArrCount := WriteSignatureArray(b, sigBlobs)

	ob := b.StartObject(SizeBaseTxFull)
	ob.SetUint8(OffsetTxKind, uint8(TxKindBaseFull))
	ob.SetUint32(OffsetBaseTxFull_NetworkID, in.NetworkID)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetBaseTxFull_BlockchainID+i, in.BlockchainID[i])
	}
	ob.SetList(OffsetBaseTxFull_OutsList, outsOff, outsCount)
	ob.SetList(OffsetBaseTxFull_InsList, insOff, insCount)
	ob.SetList(OffsetBaseTxFull_CredsList, credsOff, credsCount)
	ob.SetList(OffsetBaseTxFull_SigIndicesArr, sigIdxArrOff, sigIdxArrCount)
	ob.SetList(OffsetBaseTxFull_SigArr, sigArrOff, sigArrCount)
	ob.SetBytes(OffsetBaseTxFull_Memo, in.Memo)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return BaseTxFull{msg: msg, obj: msg.Root()}
}
