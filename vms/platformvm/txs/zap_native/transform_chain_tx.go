// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// TransformChainTx — transform chain configuration (POST-rebrand from
// TransformSubnetTx). Carries economic parameters for converting a
// chain's validator/delegator economics. ChainAuth lives in the signed
// wrapper, not here.
//
// Fixed-section layout (size 191 bytes):
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
//	Chain                        32B    @ 85
//	AssetID                      32B    @ 117
//	InitialSupply                uint64 @ 149
//	MaximumSupply                uint64 @ 157
//	MinConsumptionRate           uint64 @ 165
//	MaxConsumptionRate           uint64 @ 173
//	MinValidatorStake            uint64 @ 181
//	MaxValidatorStake            uint64 @ 189
//	MinStakeDuration             uint32 @ 197
//	MaxStakeDuration             uint32 @ 201
//	MinDelegationFee             uint32 @ 205
//	MinDelegatorStake            uint64 @ 209
//	MaxValidatorWeightFactor     uint8  @ 217
//	UptimeRequirement            uint32 @ 218
const (
	OffsetTransformChainTx_NetworkID                = 1
	OffsetTransformChainTx_BlockchainID             = 5
	OffsetTransformChainTx_OutsList                 = 37
	OffsetTransformChainTx_InsList                  = 45
	OffsetTransformChainTx_CredsList                = 53
	OffsetTransformChainTx_SigIndicesArr            = 61
	OffsetTransformChainTx_SigArr                   = 69
	OffsetTransformChainTx_Memo                     = 77
	OffsetTransformChainTx_Chain                    = 85
	OffsetTransformChainTx_AssetID                  = 117
	OffsetTransformChainTx_InitialSupply            = 149
	OffsetTransformChainTx_MaximumSupply            = 157
	OffsetTransformChainTx_MinConsumptionRate       = 165
	OffsetTransformChainTx_MaxConsumptionRate       = 173
	OffsetTransformChainTx_MinValidatorStake        = 181
	OffsetTransformChainTx_MaxValidatorStake        = 189
	OffsetTransformChainTx_MinStakeDuration         = 197
	OffsetTransformChainTx_MaxStakeDuration         = 201
	OffsetTransformChainTx_MinDelegationFee         = 205
	OffsetTransformChainTx_MinDelegatorStake        = 209
	OffsetTransformChainTx_MaxValidatorWeightFactor = 217
	OffsetTransformChainTx_UptimeRequirement        = 218
	SizeTransformChainTx                            = 222
)

type TransformChainTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t TransformChainTx) NetworkID() uint32 {
	return t.obj.Uint32(OffsetTransformChainTx_NetworkID)
}
func (t TransformChainTx) BlockchainID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetTransformChainTx_BlockchainID + i)
	}
	return out
}
func (t TransformChainTx) Outs() OutputList {
	return OutputListView(t.obj, OffsetTransformChainTx_OutsList)
}
func (t TransformChainTx) Ins() InputList {
	return InputListView(t.obj, OffsetTransformChainTx_InsList)
}
func (t TransformChainTx) Credentials() CredentialList {
	return CredentialListView(t.obj, OffsetTransformChainTx_CredsList)
}
func (t TransformChainTx) SigIndicesArray() SigIndicesArray {
	return SigIndicesArrayView(t.obj, OffsetTransformChainTx_SigIndicesArr)
}
func (t TransformChainTx) SignatureArray() SignatureArray {
	return SignatureArrayView(t.obj, OffsetTransformChainTx_SigArr)
}
func (t TransformChainTx) Memo() []byte { return t.obj.Bytes(OffsetTransformChainTx_Memo) }

func (t TransformChainTx) Chain() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetTransformChainTx_Chain + i)
	}
	return out
}
func (t TransformChainTx) AssetID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetTransformChainTx_AssetID + i)
	}
	return out
}
func (t TransformChainTx) InitialSupply() uint64 {
	return t.obj.Uint64(OffsetTransformChainTx_InitialSupply)
}
func (t TransformChainTx) MaximumSupply() uint64 {
	return t.obj.Uint64(OffsetTransformChainTx_MaximumSupply)
}
func (t TransformChainTx) MinConsumptionRate() uint64 {
	return t.obj.Uint64(OffsetTransformChainTx_MinConsumptionRate)
}
func (t TransformChainTx) MaxConsumptionRate() uint64 {
	return t.obj.Uint64(OffsetTransformChainTx_MaxConsumptionRate)
}
func (t TransformChainTx) MinValidatorStake() uint64 {
	return t.obj.Uint64(OffsetTransformChainTx_MinValidatorStake)
}
func (t TransformChainTx) MaxValidatorStake() uint64 {
	return t.obj.Uint64(OffsetTransformChainTx_MaxValidatorStake)
}
func (t TransformChainTx) MinStakeDuration() uint32 {
	return t.obj.Uint32(OffsetTransformChainTx_MinStakeDuration)
}
func (t TransformChainTx) MaxStakeDuration() uint32 {
	return t.obj.Uint32(OffsetTransformChainTx_MaxStakeDuration)
}
func (t TransformChainTx) MinDelegationFee() uint32 {
	return t.obj.Uint32(OffsetTransformChainTx_MinDelegationFee)
}
func (t TransformChainTx) MinDelegatorStake() uint64 {
	return t.obj.Uint64(OffsetTransformChainTx_MinDelegatorStake)
}
func (t TransformChainTx) MaxValidatorWeightFactor() uint8 {
	return t.obj.Uint8(OffsetTransformChainTx_MaxValidatorWeightFactor)
}
func (t TransformChainTx) UptimeRequirement() uint32 {
	return t.obj.Uint32(OffsetTransformChainTx_UptimeRequirement)
}

func (t TransformChainTx) Bytes() []byte { return t.msg.Bytes() }
func (t TransformChainTx) IsZero() bool  { return t.msg == nil }

func WrapTransformChainTx(b []byte) (TransformChainTx, error) {
	msg, obj, err := parseAndCheckKind(b, TxKindTransformChain)
	if err != nil {
		return TransformChainTx{}, err
	}
	return TransformChainTx{msg: msg, obj: obj}, nil
}

type TransformChainTxInput struct {
	NetworkID                uint32
	BlockchainID             ids.ID
	Outs                     []OutputListEntry
	Ins                      []InputListEntry
	Credentials              []CredentialListEntry
	Memo                     []byte
	Chain                    ids.ID
	AssetID                  ids.ID
	InitialSupply            uint64
	MaximumSupply            uint64
	MinConsumptionRate       uint64
	MaxConsumptionRate       uint64
	MinValidatorStake        uint64
	MaxValidatorStake        uint64
	MinStakeDuration         uint32
	MaxStakeDuration         uint32
	MinDelegationFee         uint32
	MinDelegatorStake        uint64
	MaxValidatorWeightFactor uint8
	UptimeRequirement        uint32
}

func NewTransformChainTx(in TransformChainTxInput) TransformChainTx {
	cap := zap.HeaderSize + 16 + SizeTransformChainTx
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

	ob := b.StartObject(SizeTransformChainTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindTransformChain))
	ob.SetUint32(OffsetTransformChainTx_NetworkID, in.NetworkID)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetTransformChainTx_BlockchainID+i, in.BlockchainID[i])
		ob.SetUint8(OffsetTransformChainTx_Chain+i, in.Chain[i])
		ob.SetUint8(OffsetTransformChainTx_AssetID+i, in.AssetID[i])
	}
	ob.SetList(OffsetTransformChainTx_OutsList, outsOff, outsCount)
	ob.SetList(OffsetTransformChainTx_InsList, insOff, insCount)
	ob.SetList(OffsetTransformChainTx_CredsList, credsOff, credsCount)
	ob.SetList(OffsetTransformChainTx_SigIndicesArr, sigIdxArrOff, sigIdxArrCount)
	ob.SetList(OffsetTransformChainTx_SigArr, sigArrOff, sigArrCount)
	ob.SetBytes(OffsetTransformChainTx_Memo, in.Memo)
	ob.SetUint64(OffsetTransformChainTx_InitialSupply, in.InitialSupply)
	ob.SetUint64(OffsetTransformChainTx_MaximumSupply, in.MaximumSupply)
	ob.SetUint64(OffsetTransformChainTx_MinConsumptionRate, in.MinConsumptionRate)
	ob.SetUint64(OffsetTransformChainTx_MaxConsumptionRate, in.MaxConsumptionRate)
	ob.SetUint64(OffsetTransformChainTx_MinValidatorStake, in.MinValidatorStake)
	ob.SetUint64(OffsetTransformChainTx_MaxValidatorStake, in.MaxValidatorStake)
	ob.SetUint32(OffsetTransformChainTx_MinStakeDuration, in.MinStakeDuration)
	ob.SetUint32(OffsetTransformChainTx_MaxStakeDuration, in.MaxStakeDuration)
	ob.SetUint32(OffsetTransformChainTx_MinDelegationFee, in.MinDelegationFee)
	ob.SetUint64(OffsetTransformChainTx_MinDelegatorStake, in.MinDelegatorStake)
	ob.SetUint8(OffsetTransformChainTx_MaxValidatorWeightFactor, in.MaxValidatorWeightFactor)
	ob.SetUint32(OffsetTransformChainTx_UptimeRequirement, in.UptimeRequirement)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return TransformChainTx{msg: msg, obj: msg.Root()}
}
