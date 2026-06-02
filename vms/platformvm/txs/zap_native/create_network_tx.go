// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// CreateNetworkTx — create a new network/subnet (POST-rebrand: this is
// the rebranded CreateSubnetTx). Carries the BaseTxFull spending state
// plus a single-address owner stub authorizing the network manager.
//
// Fixed-section layout (size 113 bytes):
//
//	TxKind             uint8  @ 0
//	NetworkID          uint32 @ 1
//	BlockchainID       32B    @ 5
//	OutsList           8B     @ 37
//	InsList            8B     @ 45
//	CredsList          8B     @ 53
//	SigIndicesArr      8B     @ 61
//	SigArr             8B     @ 69
//	Memo               8B     @ 77
//	OwnerThreshold     uint32 @ 85
//	OwnerLocktime      uint64 @ 89
//	OwnerAddress       20B    @ 97
const (
	OffsetCreateNetworkTx_NetworkID      = 1
	OffsetCreateNetworkTx_BlockchainID   = 5
	OffsetCreateNetworkTx_OutsList       = 37
	OffsetCreateNetworkTx_InsList        = 45
	OffsetCreateNetworkTx_CredsList      = 53
	OffsetCreateNetworkTx_SigIndicesArr  = 61
	OffsetCreateNetworkTx_SigArr         = 69
	OffsetCreateNetworkTx_Memo           = 77
	OffsetCreateNetworkTx_OwnerThreshold = 85
	OffsetCreateNetworkTx_OwnerLocktime  = 89
	OffsetCreateNetworkTx_OwnerAddress   = 97
	SizeCreateNetworkTx                  = 117
)

type CreateNetworkTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t CreateNetworkTx) NetworkID() uint32 { return t.obj.Uint32(OffsetCreateNetworkTx_NetworkID) }
func (t CreateNetworkTx) BlockchainID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetCreateNetworkTx_BlockchainID + i)
	}
	return out
}
func (t CreateNetworkTx) Outs() OutputList {
	return OutputListView(t.obj, OffsetCreateNetworkTx_OutsList)
}
func (t CreateNetworkTx) Ins() InputList {
	return InputListView(t.obj, OffsetCreateNetworkTx_InsList)
}
func (t CreateNetworkTx) Credentials() CredentialList {
	return CredentialListView(t.obj, OffsetCreateNetworkTx_CredsList)
}
func (t CreateNetworkTx) SigIndicesArray() SigIndicesArray {
	return SigIndicesArrayView(t.obj, OffsetCreateNetworkTx_SigIndicesArr)
}
func (t CreateNetworkTx) SignatureArray() SignatureArray {
	return SignatureArrayView(t.obj, OffsetCreateNetworkTx_SigArr)
}
func (t CreateNetworkTx) Memo() []byte { return t.obj.Bytes(OffsetCreateNetworkTx_Memo) }

func (t CreateNetworkTx) Owner() (uint32, uint64, ids.ShortID) {
	threshold := t.obj.Uint32(OffsetCreateNetworkTx_OwnerThreshold)
	locktime := t.obj.Uint64(OffsetCreateNetworkTx_OwnerLocktime)
	var addr ids.ShortID
	for i := 0; i < ids.ShortIDLen; i++ {
		addr[i] = t.obj.Uint8(OffsetCreateNetworkTx_OwnerAddress + i)
	}
	return threshold, locktime, addr
}

func (t CreateNetworkTx) Bytes() []byte { return t.msg.Bytes() }
func (t CreateNetworkTx) IsZero() bool  { return t.msg == nil }

func WrapCreateNetworkTx(b []byte) (CreateNetworkTx, error) {
	msg, obj, err := parseAndCheckKind(b, TxKindCreateNetwork)
	if err != nil {
		return CreateNetworkTx{}, err
	}
	return CreateNetworkTx{msg: msg, obj: obj}, nil
}

type CreateNetworkTxInput struct {
	NetworkID    uint32
	BlockchainID ids.ID
	Outs         []OutputListEntry
	Ins          []InputListEntry
	Credentials  []CredentialListEntry
	Memo         []byte
	Owner        OwnerStub
}

func NewCreateNetworkTx(in CreateNetworkTxInput) CreateNetworkTx {
	cap := zap.HeaderSize + 16 + SizeCreateNetworkTx
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

	ob := b.StartObject(SizeCreateNetworkTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindCreateNetwork))
	ob.SetUint32(OffsetCreateNetworkTx_NetworkID, in.NetworkID)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetCreateNetworkTx_BlockchainID+i, in.BlockchainID[i])
	}
	ob.SetList(OffsetCreateNetworkTx_OutsList, outsOff, outsCount)
	ob.SetList(OffsetCreateNetworkTx_InsList, insOff, insCount)
	ob.SetList(OffsetCreateNetworkTx_CredsList, credsOff, credsCount)
	ob.SetList(OffsetCreateNetworkTx_SigIndicesArr, sigIdxArrOff, sigIdxArrCount)
	ob.SetList(OffsetCreateNetworkTx_SigArr, sigArrOff, sigArrCount)
	ob.SetBytes(OffsetCreateNetworkTx_Memo, in.Memo)
	ob.SetUint32(OffsetCreateNetworkTx_OwnerThreshold, in.Owner.Threshold)
	ob.SetUint64(OffsetCreateNetworkTx_OwnerLocktime, in.Owner.Locktime)
	for i := 0; i < ids.ShortIDLen; i++ {
		ob.SetUint8(OffsetCreateNetworkTx_OwnerAddress+i, in.Owner.Address[i])
	}
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return CreateNetworkTx{msg: msg, obj: msg.Root()}
}
