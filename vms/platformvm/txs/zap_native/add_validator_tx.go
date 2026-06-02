// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// AddValidatorTx is the pre-Etna primary-network validator add. Mostly
// superseded by AddPermissionlessValidatorTx but still present in legacy
// blocks. Single-address rewards owner (OwnerStub fast path); multi-address
// callers go through legacy codec or build Owner explicitly.
//
// Fixed-section layout (size 195 bytes):
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
//	StakeOutsList           8B     @ 129  (sibling Outs for staked tokens)
//	RewardsOwnerThreshold   uint32 @ 137
//	RewardsOwnerLocktime    uint64 @ 141
//	RewardsOwnerAddress     20B    @ 149
//	DelegationShares        uint32 @ 169
const (
	OffsetAddValidatorTx_NetworkID             = 1
	OffsetAddValidatorTx_BlockchainID          = 5
	OffsetAddValidatorTx_OutsList              = 37
	OffsetAddValidatorTx_InsList               = 45
	OffsetAddValidatorTx_CredsList             = 53
	OffsetAddValidatorTx_SigIndicesArr         = 61
	OffsetAddValidatorTx_SigArr                = 69
	OffsetAddValidatorTx_Memo                  = 77
	OffsetAddValidatorTx_NodeID                = 85
	OffsetAddValidatorTx_StakeStart            = 105
	OffsetAddValidatorTx_StakeEnd              = 113
	OffsetAddValidatorTx_StakeWeight           = 121
	OffsetAddValidatorTx_StakeOutsList         = 129
	OffsetAddValidatorTx_RewardsOwnerThreshold = 137
	OffsetAddValidatorTx_RewardsOwnerLocktime  = 141
	OffsetAddValidatorTx_RewardsOwnerAddress   = 149
	OffsetAddValidatorTx_DelegationShares      = 169
	SizeAddValidatorTx                         = 173
)

type AddValidatorTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t AddValidatorTx) NetworkID() uint32 { return t.obj.Uint32(OffsetAddValidatorTx_NetworkID) }
func (t AddValidatorTx) BlockchainID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetAddValidatorTx_BlockchainID + i)
	}
	return out
}
func (t AddValidatorTx) Outs() OutputList { return OutputListView(t.obj, OffsetAddValidatorTx_OutsList) }
func (t AddValidatorTx) Ins() InputList   { return InputListView(t.obj, OffsetAddValidatorTx_InsList) }
func (t AddValidatorTx) Credentials() CredentialList {
	return CredentialListView(t.obj, OffsetAddValidatorTx_CredsList)
}
func (t AddValidatorTx) SigIndicesArray() SigIndicesArray {
	return SigIndicesArrayView(t.obj, OffsetAddValidatorTx_SigIndicesArr)
}
func (t AddValidatorTx) SignatureArray() SignatureArray {
	return SignatureArrayView(t.obj, OffsetAddValidatorTx_SigArr)
}
func (t AddValidatorTx) Memo() []byte { return t.obj.Bytes(OffsetAddValidatorTx_Memo) }

func (t AddValidatorTx) NodeID() ids.NodeID {
	var out ids.NodeID
	for i := 0; i < ids.ShortIDLen; i++ {
		out[i] = t.obj.Uint8(OffsetAddValidatorTx_NodeID + i)
	}
	return out
}
func (t AddValidatorTx) StakeStart() uint64  { return t.obj.Uint64(OffsetAddValidatorTx_StakeStart) }
func (t AddValidatorTx) StakeEnd() uint64    { return t.obj.Uint64(OffsetAddValidatorTx_StakeEnd) }
func (t AddValidatorTx) StakeWeight() uint64 { return t.obj.Uint64(OffsetAddValidatorTx_StakeWeight) }
func (t AddValidatorTx) StakeOuts() OutputList {
	return OutputListView(t.obj, OffsetAddValidatorTx_StakeOutsList)
}

func (t AddValidatorTx) RewardsOwner() (uint32, uint64, ids.ShortID) {
	threshold := t.obj.Uint32(OffsetAddValidatorTx_RewardsOwnerThreshold)
	locktime := t.obj.Uint64(OffsetAddValidatorTx_RewardsOwnerLocktime)
	var addr ids.ShortID
	for i := 0; i < ids.ShortIDLen; i++ {
		addr[i] = t.obj.Uint8(OffsetAddValidatorTx_RewardsOwnerAddress + i)
	}
	return threshold, locktime, addr
}

func (t AddValidatorTx) DelegationShares() uint32 {
	return t.obj.Uint32(OffsetAddValidatorTx_DelegationShares)
}

func (t AddValidatorTx) Bytes() []byte { return t.msg.Bytes() }
func (t AddValidatorTx) IsZero() bool  { return t.msg == nil }

func WrapAddValidatorTx(b []byte) (AddValidatorTx, error) {
	msg, obj, err := parseAndCheckKind(b, TxKindAddValidator)
	if err != nil {
		return AddValidatorTx{}, err
	}
	return AddValidatorTx{msg: msg, obj: obj}, nil
}

type AddValidatorTxInput struct {
	NetworkID        uint32
	BlockchainID     ids.ID
	Outs             []OutputListEntry
	Ins              []InputListEntry
	Credentials      []CredentialListEntry
	Memo             []byte
	NodeID           ids.NodeID
	StakeStart       uint64
	StakeEnd         uint64
	StakeWeight      uint64
	StakeOuts        []OutputListEntry
	RewardsOwner     OwnerStub
	DelegationShares uint32
}

func NewAddValidatorTx(in AddValidatorTxInput) AddValidatorTx {
	cap := zap.HeaderSize + 16 + SizeAddValidatorTx
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

	ob := b.StartObject(SizeAddValidatorTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindAddValidator))
	ob.SetUint32(OffsetAddValidatorTx_NetworkID, in.NetworkID)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetAddValidatorTx_BlockchainID+i, in.BlockchainID[i])
	}
	ob.SetList(OffsetAddValidatorTx_OutsList, outsOff, outsCount)
	ob.SetList(OffsetAddValidatorTx_InsList, insOff, insCount)
	ob.SetList(OffsetAddValidatorTx_CredsList, credsOff, credsCount)
	ob.SetList(OffsetAddValidatorTx_SigIndicesArr, sigIdxArrOff, sigIdxArrCount)
	ob.SetList(OffsetAddValidatorTx_SigArr, sigArrOff, sigArrCount)
	ob.SetBytes(OffsetAddValidatorTx_Memo, in.Memo)
	for i := 0; i < ids.ShortIDLen; i++ {
		ob.SetUint8(OffsetAddValidatorTx_NodeID+i, in.NodeID[i])
		ob.SetUint8(OffsetAddValidatorTx_RewardsOwnerAddress+i, in.RewardsOwner.Address[i])
	}
	ob.SetUint64(OffsetAddValidatorTx_StakeStart, in.StakeStart)
	ob.SetUint64(OffsetAddValidatorTx_StakeEnd, in.StakeEnd)
	ob.SetUint64(OffsetAddValidatorTx_StakeWeight, in.StakeWeight)
	ob.SetList(OffsetAddValidatorTx_StakeOutsList, stakeOutsOff, stakeOutsCount)
	ob.SetUint32(OffsetAddValidatorTx_RewardsOwnerThreshold, in.RewardsOwner.Threshold)
	ob.SetUint64(OffsetAddValidatorTx_RewardsOwnerLocktime, in.RewardsOwner.Locktime)
	ob.SetUint32(OffsetAddValidatorTx_DelegationShares, in.DelegationShares)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return AddValidatorTx{msg: msg, obj: msg.Root()}
}
