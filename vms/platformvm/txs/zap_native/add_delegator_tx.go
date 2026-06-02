// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// AddDelegatorTx is the pre-Etna primary-network delegator add. Mostly
// superseded by AddPermissionlessDelegatorTx but still present in legacy
// blocks. Single delegation-rewards owner stub.
//
// Fixed-section layout (size 173 bytes):
//
//	TxKind                       uint8  @ 0
//	NetworkID                    uint32 @ 1
//	BlockchainID                 32B    @ 5
//	OutsList                     8B     @ 37
//	InsList                      8B     @ 45
//	CredsList                    8B     @ 53
//	SigIndicesArr                8B     @ 61
//	SigArr                       8B     @ 69
//	Memo                         8B     @ 77
//	NodeID                       20B    @ 85
//	StakeStart                   uint64 @ 105
//	StakeEnd                     uint64 @ 113
//	StakeWeight                  uint64 @ 121
//	StakeOutsList                8B     @ 129
//	DelegationRewardsThreshold   uint32 @ 137
//	DelegationRewardsLocktime    uint64 @ 141
//	DelegationRewardsAddress     20B    @ 149
const (
	OffsetAddDelegatorTx_NetworkID                  = 1
	OffsetAddDelegatorTx_BlockchainID               = 5
	OffsetAddDelegatorTx_OutsList                   = 37
	OffsetAddDelegatorTx_InsList                    = 45
	OffsetAddDelegatorTx_CredsList                  = 53
	OffsetAddDelegatorTx_SigIndicesArr              = 61
	OffsetAddDelegatorTx_SigArr                     = 69
	OffsetAddDelegatorTx_Memo                       = 77
	OffsetAddDelegatorTx_NodeID                     = 85
	OffsetAddDelegatorTx_StakeStart                 = 105
	OffsetAddDelegatorTx_StakeEnd                   = 113
	OffsetAddDelegatorTx_StakeWeight                = 121
	OffsetAddDelegatorTx_StakeOutsList              = 129
	OffsetAddDelegatorTx_DelegationRewardsThreshold = 137
	OffsetAddDelegatorTx_DelegationRewardsLocktime  = 141
	OffsetAddDelegatorTx_DelegationRewardsAddress   = 149
	SizeAddDelegatorTx                              = 169
)

type AddDelegatorTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t AddDelegatorTx) NetworkID() uint32 { return t.obj.Uint32(OffsetAddDelegatorTx_NetworkID) }
func (t AddDelegatorTx) BlockchainID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetAddDelegatorTx_BlockchainID + i)
	}
	return out
}
func (t AddDelegatorTx) Outs() OutputList { return OutputListView(t.obj, OffsetAddDelegatorTx_OutsList) }
func (t AddDelegatorTx) Ins() InputList   { return InputListView(t.obj, OffsetAddDelegatorTx_InsList) }
func (t AddDelegatorTx) Credentials() CredentialList {
	return CredentialListView(t.obj, OffsetAddDelegatorTx_CredsList)
}
func (t AddDelegatorTx) SigIndicesArray() SigIndicesArray {
	return SigIndicesArrayView(t.obj, OffsetAddDelegatorTx_SigIndicesArr)
}
func (t AddDelegatorTx) SignatureArray() SignatureArray {
	return SignatureArrayView(t.obj, OffsetAddDelegatorTx_SigArr)
}
func (t AddDelegatorTx) Memo() []byte { return t.obj.Bytes(OffsetAddDelegatorTx_Memo) }

func (t AddDelegatorTx) NodeID() ids.NodeID {
	var out ids.NodeID
	for i := 0; i < ids.ShortIDLen; i++ {
		out[i] = t.obj.Uint8(OffsetAddDelegatorTx_NodeID + i)
	}
	return out
}
func (t AddDelegatorTx) StakeStart() uint64  { return t.obj.Uint64(OffsetAddDelegatorTx_StakeStart) }
func (t AddDelegatorTx) StakeEnd() uint64    { return t.obj.Uint64(OffsetAddDelegatorTx_StakeEnd) }
func (t AddDelegatorTx) StakeWeight() uint64 { return t.obj.Uint64(OffsetAddDelegatorTx_StakeWeight) }
func (t AddDelegatorTx) StakeOuts() OutputList {
	return OutputListView(t.obj, OffsetAddDelegatorTx_StakeOutsList)
}

func (t AddDelegatorTx) DelegationRewardsOwner() (uint32, uint64, ids.ShortID) {
	threshold := t.obj.Uint32(OffsetAddDelegatorTx_DelegationRewardsThreshold)
	locktime := t.obj.Uint64(OffsetAddDelegatorTx_DelegationRewardsLocktime)
	var addr ids.ShortID
	for i := 0; i < ids.ShortIDLen; i++ {
		addr[i] = t.obj.Uint8(OffsetAddDelegatorTx_DelegationRewardsAddress + i)
	}
	return threshold, locktime, addr
}

func (t AddDelegatorTx) Bytes() []byte { return t.msg.Bytes() }
func (t AddDelegatorTx) IsZero() bool  { return t.msg == nil }

func WrapAddDelegatorTx(b []byte) (AddDelegatorTx, error) {
	msg, obj, err := parseAndCheckKind(b, TxKindAddDelegator)
	if err != nil {
		return AddDelegatorTx{}, err
	}
	return AddDelegatorTx{msg: msg, obj: obj}, nil
}

type AddDelegatorTxInput struct {
	NetworkID              uint32
	BlockchainID           ids.ID
	Outs                   []OutputListEntry
	Ins                    []InputListEntry
	Credentials            []CredentialListEntry
	Memo                   []byte
	NodeID                 ids.NodeID
	StakeStart             uint64
	StakeEnd               uint64
	StakeWeight            uint64
	StakeOuts              []OutputListEntry
	DelegationRewardsOwner OwnerStub
}

func NewAddDelegatorTx(in AddDelegatorTxInput) AddDelegatorTx {
	cap := zap.HeaderSize + 16 + SizeAddDelegatorTx
	cap += (len(in.Outs) + len(in.StakeOuts)) * SizeTransferableOutput
	cap += len(in.Ins) * SizeTransferableInput
	cap += len(in.Credentials) * SizeCredential
	cap += len(in.Memo)
	b := zap.NewBuilder(cap)

	outsOff, outsCount := WriteOutputList(b, in.Outs)
	insOff, insCount, sigIndices := WriteInputList(b, in.Ins)
	credsOff, credsCount, sigBlobs := WriteCredentialList(b, in.Credentials)
	sigIdxArrOff, sigIdxArrCount := WriteSigIndicesArray(b, sigIndices)
	sigArrOff, sigArrCount := WriteSignatureArray(b, sigBlobs)
	stakeOutsOff, stakeOutsCount := WriteOutputList(b, in.StakeOuts)

	ob := b.StartObject(SizeAddDelegatorTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindAddDelegator))
	ob.SetUint32(OffsetAddDelegatorTx_NetworkID, in.NetworkID)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetAddDelegatorTx_BlockchainID+i, in.BlockchainID[i])
	}
	ob.SetList(OffsetAddDelegatorTx_OutsList, outsOff, outsCount)
	ob.SetList(OffsetAddDelegatorTx_InsList, insOff, insCount)
	ob.SetList(OffsetAddDelegatorTx_CredsList, credsOff, credsCount)
	ob.SetList(OffsetAddDelegatorTx_SigIndicesArr, sigIdxArrOff, sigIdxArrCount)
	ob.SetList(OffsetAddDelegatorTx_SigArr, sigArrOff, sigArrCount)
	ob.SetBytes(OffsetAddDelegatorTx_Memo, in.Memo)
	for i := 0; i < ids.ShortIDLen; i++ {
		ob.SetUint8(OffsetAddDelegatorTx_NodeID+i, in.NodeID[i])
		ob.SetUint8(OffsetAddDelegatorTx_DelegationRewardsAddress+i, in.DelegationRewardsOwner.Address[i])
	}
	ob.SetUint64(OffsetAddDelegatorTx_StakeStart, in.StakeStart)
	ob.SetUint64(OffsetAddDelegatorTx_StakeEnd, in.StakeEnd)
	ob.SetUint64(OffsetAddDelegatorTx_StakeWeight, in.StakeWeight)
	ob.SetList(OffsetAddDelegatorTx_StakeOutsList, stakeOutsOff, stakeOutsCount)
	ob.SetUint32(OffsetAddDelegatorTx_DelegationRewardsThreshold, in.DelegationRewardsOwner.Threshold)
	ob.SetUint64(OffsetAddDelegatorTx_DelegationRewardsLocktime, in.DelegationRewardsOwner.Locktime)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return AddDelegatorTx{msg: msg, obj: msg.Root()}
}
