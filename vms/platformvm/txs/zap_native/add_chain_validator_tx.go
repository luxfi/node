// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// AddChainValidatorTx — chain/subnet validator add. POST-rebrand: this is
// the rebranded AddSubnetValidatorTx. The legacy code has already shipped
// the rename in the txs package (verified: no `Subnet` in *.go names);
// our v3 form pins ChainValidator semantics. ChainAuth (credential proof)
// lives in the signed-tx wrapper, not in the unsigned encoding here.
//
// Fixed-section layout (size 165 bytes):
//
//	TxKind                  uint8  @ 0
//	NetworkID               uint32 @ 1
//	BlockchainID            32B    @ 5
//	OutsList                8B     @ 37
//	InsList                 8B     @ 45
//	CredsList               8B     @ 53
//	SigIndicesArr           8B     @ 61
//	SigArr                  8B     @ 69
//	Memo                    8B     @ 77
//	NodeID                  20B    @ 85
//	StakeStart              uint64 @ 105
//	StakeEnd                uint64 @ 113
//	StakeWeight             uint64 @ 121
//	Chain                   32B    @ 129  (ids.ID — the chain this validator joins)
const (
	OffsetAddChainValidatorTx_NetworkID    = 1
	OffsetAddChainValidatorTx_BlockchainID = 5
	OffsetAddChainValidatorTx_OutsList     = 37
	OffsetAddChainValidatorTx_InsList      = 45
	OffsetAddChainValidatorTx_CredsList    = 53
	OffsetAddChainValidatorTx_SigIndicesArr = 61
	OffsetAddChainValidatorTx_SigArr       = 69
	OffsetAddChainValidatorTx_Memo         = 77
	OffsetAddChainValidatorTx_NodeID       = 85
	OffsetAddChainValidatorTx_StakeStart   = 105
	OffsetAddChainValidatorTx_StakeEnd     = 113
	OffsetAddChainValidatorTx_StakeWeight  = 121
	OffsetAddChainValidatorTx_Chain        = 129
	SizeAddChainValidatorTx                = 161
)

type AddChainValidatorTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t AddChainValidatorTx) NetworkID() uint32 {
	return t.obj.Uint32(OffsetAddChainValidatorTx_NetworkID)
}
func (t AddChainValidatorTx) BlockchainID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetAddChainValidatorTx_BlockchainID + i)
	}
	return out
}
func (t AddChainValidatorTx) Outs() OutputList {
	return OutputListView(t.obj, OffsetAddChainValidatorTx_OutsList)
}
func (t AddChainValidatorTx) Ins() InputList {
	return InputListView(t.obj, OffsetAddChainValidatorTx_InsList)
}
func (t AddChainValidatorTx) Credentials() CredentialList {
	return CredentialListView(t.obj, OffsetAddChainValidatorTx_CredsList)
}
func (t AddChainValidatorTx) SigIndicesArray() SigIndicesArray {
	return SigIndicesArrayView(t.obj, OffsetAddChainValidatorTx_SigIndicesArr)
}
func (t AddChainValidatorTx) SignatureArray() SignatureArray {
	return SignatureArrayView(t.obj, OffsetAddChainValidatorTx_SigArr)
}
func (t AddChainValidatorTx) Memo() []byte {
	return t.obj.Bytes(OffsetAddChainValidatorTx_Memo)
}
func (t AddChainValidatorTx) NodeID() ids.NodeID {
	var out ids.NodeID
	for i := 0; i < ids.ShortIDLen; i++ {
		out[i] = t.obj.Uint8(OffsetAddChainValidatorTx_NodeID + i)
	}
	return out
}
func (t AddChainValidatorTx) StakeStart() uint64 {
	return t.obj.Uint64(OffsetAddChainValidatorTx_StakeStart)
}
func (t AddChainValidatorTx) StakeEnd() uint64 {
	return t.obj.Uint64(OffsetAddChainValidatorTx_StakeEnd)
}
func (t AddChainValidatorTx) StakeWeight() uint64 {
	return t.obj.Uint64(OffsetAddChainValidatorTx_StakeWeight)
}
func (t AddChainValidatorTx) Chain() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetAddChainValidatorTx_Chain + i)
	}
	return out
}

func (t AddChainValidatorTx) Bytes() []byte { return t.msg.Bytes() }
func (t AddChainValidatorTx) IsZero() bool  { return t.msg == nil }

func WrapAddChainValidatorTx(b []byte) (AddChainValidatorTx, error) {
	msg, obj, err := parseAndCheckKind(b, TxKindAddChainValidator)
	if err != nil {
		return AddChainValidatorTx{}, err
	}
	return AddChainValidatorTx{msg: msg, obj: obj}, nil
}

type AddChainValidatorTxInput struct {
	NetworkID    uint32
	BlockchainID ids.ID
	Outs         []OutputListEntry
	Ins          []InputListEntry
	Credentials  []CredentialListEntry
	Memo         []byte
	NodeID       ids.NodeID
	StakeStart   uint64
	StakeEnd     uint64
	StakeWeight  uint64
	Chain        ids.ID
}

func NewAddChainValidatorTx(in AddChainValidatorTxInput) AddChainValidatorTx {
	cap := zap.HeaderSize + 16 + SizeAddChainValidatorTx
	cap += len(in.Outs) * SizeTransferableOutput
	cap += len(in.Ins) * SizeTransferableInput
	cap += len(in.Credentials) * SizeCredential
	cap += len(in.Memo)
	b := zap.NewBuilder(cap)

	outsOff, outsCount := WriteOutputList(b, in.Outs)
	insOff, insCount, sigIndices := WriteInputList(b, in.Ins)
	credsOff, credsCount, sigBlobs := WriteCredentialList(b, in.Credentials)
	sigIdxArrOff, sigIdxArrCount := WriteSigIndicesArray(b, sigIndices)
	sigArrOff, sigArrCount := WriteSignatureArray(b, sigBlobs)

	ob := b.StartObject(SizeAddChainValidatorTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindAddChainValidator))
	ob.SetUint32(OffsetAddChainValidatorTx_NetworkID, in.NetworkID)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetAddChainValidatorTx_BlockchainID+i, in.BlockchainID[i])
		ob.SetUint8(OffsetAddChainValidatorTx_Chain+i, in.Chain[i])
	}
	ob.SetList(OffsetAddChainValidatorTx_OutsList, outsOff, outsCount)
	ob.SetList(OffsetAddChainValidatorTx_InsList, insOff, insCount)
	ob.SetList(OffsetAddChainValidatorTx_CredsList, credsOff, credsCount)
	ob.SetList(OffsetAddChainValidatorTx_SigIndicesArr, sigIdxArrOff, sigIdxArrCount)
	ob.SetList(OffsetAddChainValidatorTx_SigArr, sigArrOff, sigArrCount)
	ob.SetBytes(OffsetAddChainValidatorTx_Memo, in.Memo)
	for i := 0; i < ids.ShortIDLen; i++ {
		ob.SetUint8(OffsetAddChainValidatorTx_NodeID+i, in.NodeID[i])
	}
	ob.SetUint64(OffsetAddChainValidatorTx_StakeStart, in.StakeStart)
	ob.SetUint64(OffsetAddChainValidatorTx_StakeEnd, in.StakeEnd)
	ob.SetUint64(OffsetAddChainValidatorTx_StakeWeight, in.StakeWeight)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return AddChainValidatorTx{msg: msg, obj: msg.Root()}
}
