// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// AddPermissionlessDelegatorTx — Etna+ permissionless delegator add. Carries
// a chain ID (which chain the validator is on), stake details, and a single
// delegation-rewards owner stub.
//
// Fixed-section layout (size 217 bytes):
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
//	Chain                        32B    @ 129  (ids.ID — which chain the validator validates)
//	StakeAssetID                 32B    @ 161
//	StakeOutsList                8B     @ 193
//	DelegationRewardsThreshold   uint32 @ 201
//	DelegationRewardsLocktime    uint64 @ 205
//	DelegationRewardsAddress     20B    @ 213
const (
	OffsetAPDTx_NetworkID                  = 1
	OffsetAPDTx_BlockchainID               = 5
	OffsetAPDTx_OutsList                   = 37
	OffsetAPDTx_InsList                    = 45
	OffsetAPDTx_CredsList                  = 53
	OffsetAPDTx_SigIndicesArr              = 61
	OffsetAPDTx_SigArr                     = 69
	OffsetAPDTx_Memo                       = 77
	OffsetAPDTx_NodeID                     = 85
	OffsetAPDTx_StakeStart                 = 105
	OffsetAPDTx_StakeEnd                   = 113
	OffsetAPDTx_StakeWeight                = 121
	OffsetAPDTx_Chain                      = 129
	OffsetAPDTx_StakeAssetID               = 161
	OffsetAPDTx_StakeOutsList              = 193
	OffsetAPDTx_DelegationRewardsThreshold = 201
	OffsetAPDTx_DelegationRewardsLocktime  = 205
	OffsetAPDTx_DelegationRewardsAddress   = 213
	SizeAddPermissionlessDelegatorTx       = 233
)

type AddPermissionlessDelegatorTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t AddPermissionlessDelegatorTx) NetworkID() uint32 { return t.obj.Uint32(OffsetAPDTx_NetworkID) }
func (t AddPermissionlessDelegatorTx) BlockchainID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetAPDTx_BlockchainID + i)
	}
	return out
}
func (t AddPermissionlessDelegatorTx) Outs() OutputList {
	return OutputListView(t.obj, OffsetAPDTx_OutsList)
}
func (t AddPermissionlessDelegatorTx) Ins() InputList {
	return InputListView(t.obj, OffsetAPDTx_InsList)
}
func (t AddPermissionlessDelegatorTx) Credentials() CredentialList {
	return CredentialListView(t.obj, OffsetAPDTx_CredsList)
}
func (t AddPermissionlessDelegatorTx) SigIndicesArray() SigIndicesArray {
	return SigIndicesArrayView(t.obj, OffsetAPDTx_SigIndicesArr)
}
func (t AddPermissionlessDelegatorTx) SignatureArray() SignatureArray {
	return SignatureArrayView(t.obj, OffsetAPDTx_SigArr)
}
func (t AddPermissionlessDelegatorTx) Memo() []byte { return t.obj.Bytes(OffsetAPDTx_Memo) }

func (t AddPermissionlessDelegatorTx) NodeID() ids.NodeID {
	var out ids.NodeID
	for i := 0; i < ids.ShortIDLen; i++ {
		out[i] = t.obj.Uint8(OffsetAPDTx_NodeID + i)
	}
	return out
}
func (t AddPermissionlessDelegatorTx) StakeStart() uint64 {
	return t.obj.Uint64(OffsetAPDTx_StakeStart)
}
func (t AddPermissionlessDelegatorTx) StakeEnd() uint64 { return t.obj.Uint64(OffsetAPDTx_StakeEnd) }
func (t AddPermissionlessDelegatorTx) StakeWeight() uint64 {
	return t.obj.Uint64(OffsetAPDTx_StakeWeight)
}
func (t AddPermissionlessDelegatorTx) Chain() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetAPDTx_Chain + i)
	}
	return out
}
func (t AddPermissionlessDelegatorTx) StakeAssetID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetAPDTx_StakeAssetID + i)
	}
	return out
}
func (t AddPermissionlessDelegatorTx) StakeOuts() OutputList {
	return OutputListView(t.obj, OffsetAPDTx_StakeOutsList)
}

func (t AddPermissionlessDelegatorTx) DelegationRewardsOwner() (uint32, uint64, ids.ShortID) {
	threshold := t.obj.Uint32(OffsetAPDTx_DelegationRewardsThreshold)
	locktime := t.obj.Uint64(OffsetAPDTx_DelegationRewardsLocktime)
	var addr ids.ShortID
	for i := 0; i < ids.ShortIDLen; i++ {
		addr[i] = t.obj.Uint8(OffsetAPDTx_DelegationRewardsAddress + i)
	}
	return threshold, locktime, addr
}

func (t AddPermissionlessDelegatorTx) Bytes() []byte { return t.msg.Bytes() }
func (t AddPermissionlessDelegatorTx) IsZero() bool  { return t.msg == nil }

func WrapAddPermissionlessDelegatorTx(b []byte) (AddPermissionlessDelegatorTx, error) {
	msg, obj, err := parseAndCheckKind(b, TxKindAddPermissionlessDelegator)
	if err != nil {
		return AddPermissionlessDelegatorTx{}, err
	}
	return AddPermissionlessDelegatorTx{msg: msg, obj: obj}, nil
}

type AddPermissionlessDelegatorTxInput struct {
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
	Chain                  ids.ID
	StakeAssetID           ids.ID
	StakeOuts              []OutputListEntry
	DelegationRewardsOwner OwnerStub
}

func NewAddPermissionlessDelegatorTx(in AddPermissionlessDelegatorTxInput) AddPermissionlessDelegatorTx {
	cap := zap.HeaderSize + 16 + SizeAddPermissionlessDelegatorTx
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

	ob := b.StartObject(SizeAddPermissionlessDelegatorTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindAddPermissionlessDelegator))
	ob.SetUint32(OffsetAPDTx_NetworkID, in.NetworkID)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetAPDTx_BlockchainID+i, in.BlockchainID[i])
		ob.SetUint8(OffsetAPDTx_Chain+i, in.Chain[i])
		ob.SetUint8(OffsetAPDTx_StakeAssetID+i, in.StakeAssetID[i])
	}
	ob.SetList(OffsetAPDTx_OutsList, outsOff, outsCount)
	ob.SetList(OffsetAPDTx_InsList, insOff, insCount)
	ob.SetList(OffsetAPDTx_CredsList, credsOff, credsCount)
	ob.SetList(OffsetAPDTx_SigIndicesArr, sigIdxArrOff, sigIdxArrCount)
	ob.SetList(OffsetAPDTx_SigArr, sigArrOff, sigArrCount)
	ob.SetBytes(OffsetAPDTx_Memo, in.Memo)
	for i := 0; i < ids.ShortIDLen; i++ {
		ob.SetUint8(OffsetAPDTx_NodeID+i, in.NodeID[i])
		ob.SetUint8(OffsetAPDTx_DelegationRewardsAddress+i, in.DelegationRewardsOwner.Address[i])
	}
	ob.SetUint64(OffsetAPDTx_StakeStart, in.StakeStart)
	ob.SetUint64(OffsetAPDTx_StakeEnd, in.StakeEnd)
	ob.SetUint64(OffsetAPDTx_StakeWeight, in.StakeWeight)
	ob.SetList(OffsetAPDTx_StakeOutsList, stakeOutsOff, stakeOutsCount)
	ob.SetUint32(OffsetAPDTx_DelegationRewardsThreshold, in.DelegationRewardsOwner.Threshold)
	ob.SetUint64(OffsetAPDTx_DelegationRewardsLocktime, in.DelegationRewardsOwner.Locktime)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return AddPermissionlessDelegatorTx{msg: msg, obj: msg.Root()}
}
