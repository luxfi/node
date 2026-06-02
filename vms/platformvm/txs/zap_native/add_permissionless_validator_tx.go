// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// AddPermissionlessValidatorTx is the v3 register-permissionless-validator
// envelope. It carries the BaseTxFull spending state, the validator
// identity (NodeID + bond stake metadata + signer), and two nested Owner
// stubs (validation rewards owner + delegation rewards owner). Both owners
// are single-address stubs in v3 (matching TransferChainOwnershipTx /
// OutputList semantics). Multi-address owners flow through legacy.
//
// Fixed-section layout (size 269 bytes):
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
//	NodeID                       20B    @ 85    (ids.NodeID)
//	StakeStart                   uint64 @ 105
//	StakeEnd                     uint64 @ 113
//	StakeWeight                  uint64 @ 121
//	StakeAssetID                 32B    @ 129
//	StakeAmount                  uint64 @ 161
//	BLSPublicKey                 48B    @ 169   (bls.PublicKeyLen)
//	BLSProofOfPossession         96B    @ 217   (bls.SignatureLen)
//	ValidationRewardsOwnerThresh uint32 @ 313
//	ValidationRewardsOwnerLT     uint64 @ 317
//	ValidationRewardsOwnerAddr   20B    @ 325
//	DelegationRewardsOwnerThresh uint32 @ 345
//	DelegationRewardsOwnerLT     uint64 @ 349
//	DelegationRewardsOwnerAddr   20B    @ 357
//	DelegationShares             uint32 @ 377
//	Size                                = 381
const (
	OffsetAPVTx_NetworkID                       = 1
	OffsetAPVTx_BlockchainID                    = 5
	OffsetAPVTx_OutsList                        = 37
	OffsetAPVTx_InsList                         = 45
	OffsetAPVTx_CredsList                       = 53
	OffsetAPVTx_SigIndicesArr                   = 61
	OffsetAPVTx_SigArr                          = 69
	OffsetAPVTx_Memo                            = 77
	OffsetAPVTx_NodeID                          = 85
	OffsetAPVTx_StakeStart                      = 105
	OffsetAPVTx_StakeEnd                        = 113
	OffsetAPVTx_StakeWeight                     = 121
	OffsetAPVTx_StakeAssetID                    = 129
	OffsetAPVTx_StakeAmount                     = 161
	OffsetAPVTx_BLSPublicKey                    = 169
	OffsetAPVTx_BLSProofOfPossession            = OffsetAPVTx_BLSPublicKey + bls.PublicKeyLen
	OffsetAPVTx_ValidationRewardsOwnerThreshold = OffsetAPVTx_BLSProofOfPossession + bls.SignatureLen
	OffsetAPVTx_ValidationRewardsOwnerLocktime  = OffsetAPVTx_ValidationRewardsOwnerThreshold + 4
	OffsetAPVTx_ValidationRewardsOwnerAddress   = OffsetAPVTx_ValidationRewardsOwnerLocktime + 8
	OffsetAPVTx_DelegationRewardsOwnerThreshold = OffsetAPVTx_ValidationRewardsOwnerAddress + ids.ShortIDLen
	OffsetAPVTx_DelegationRewardsOwnerLocktime  = OffsetAPVTx_DelegationRewardsOwnerThreshold + 4
	OffsetAPVTx_DelegationRewardsOwnerAddress   = OffsetAPVTx_DelegationRewardsOwnerLocktime + 8
	OffsetAPVTx_DelegationShares                = OffsetAPVTx_DelegationRewardsOwnerAddress + ids.ShortIDLen
	SizeAddPermissionlessValidatorTx            = OffsetAPVTx_DelegationShares + 4
)

// AddPermissionlessValidatorTx is the zero-copy accessor.
type AddPermissionlessValidatorTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t AddPermissionlessValidatorTx) NetworkID() uint32 {
	return t.obj.Uint32(OffsetAPVTx_NetworkID)
}

func (t AddPermissionlessValidatorTx) BlockchainID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetAPVTx_BlockchainID + i)
	}
	return out
}

func (t AddPermissionlessValidatorTx) Outs() OutputList {
	return OutputListView(t.obj, OffsetAPVTx_OutsList)
}
func (t AddPermissionlessValidatorTx) Ins() InputList {
	return InputListView(t.obj, OffsetAPVTx_InsList)
}
func (t AddPermissionlessValidatorTx) Credentials() CredentialList {
	return CredentialListView(t.obj, OffsetAPVTx_CredsList)
}
func (t AddPermissionlessValidatorTx) SigIndicesArray() SigIndicesArray {
	return SigIndicesArrayView(t.obj, OffsetAPVTx_SigIndicesArr)
}
func (t AddPermissionlessValidatorTx) SignatureArray() SignatureArray {
	return SignatureArrayView(t.obj, OffsetAPVTx_SigArr)
}

func (t AddPermissionlessValidatorTx) Memo() []byte {
	return t.obj.Bytes(OffsetAPVTx_Memo)
}

func (t AddPermissionlessValidatorTx) NodeID() ids.NodeID {
	var out ids.NodeID
	for i := 0; i < ids.ShortIDLen; i++ {
		out[i] = t.obj.Uint8(OffsetAPVTx_NodeID + i)
	}
	return out
}

func (t AddPermissionlessValidatorTx) StakeStart() uint64 {
	return t.obj.Uint64(OffsetAPVTx_StakeStart)
}
func (t AddPermissionlessValidatorTx) StakeEnd() uint64 {
	return t.obj.Uint64(OffsetAPVTx_StakeEnd)
}
func (t AddPermissionlessValidatorTx) StakeWeight() uint64 {
	return t.obj.Uint64(OffsetAPVTx_StakeWeight)
}

func (t AddPermissionlessValidatorTx) StakeAssetID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetAPVTx_StakeAssetID + i)
	}
	return out
}

func (t AddPermissionlessValidatorTx) StakeAmount() uint64 {
	return t.obj.Uint64(OffsetAPVTx_StakeAmount)
}

func (t AddPermissionlessValidatorTx) BLSPublicKey() [bls.PublicKeyLen]byte {
	var out [bls.PublicKeyLen]byte
	for i := 0; i < bls.PublicKeyLen; i++ {
		out[i] = t.obj.Uint8(OffsetAPVTx_BLSPublicKey + i)
	}
	return out
}

func (t AddPermissionlessValidatorTx) BLSProofOfPossession() [bls.SignatureLen]byte {
	var out [bls.SignatureLen]byte
	for i := 0; i < bls.SignatureLen; i++ {
		out[i] = t.obj.Uint8(OffsetAPVTx_BLSProofOfPossession + i)
	}
	return out
}

// ValidationRewardsOwner returns the (threshold, locktime, address) tuple
// for the validation-rewards owner. Single-address stub form (v3).
func (t AddPermissionlessValidatorTx) ValidationRewardsOwner() (uint32, uint64, ids.ShortID) {
	threshold := t.obj.Uint32(OffsetAPVTx_ValidationRewardsOwnerThreshold)
	locktime := t.obj.Uint64(OffsetAPVTx_ValidationRewardsOwnerLocktime)
	var addr ids.ShortID
	for i := 0; i < ids.ShortIDLen; i++ {
		addr[i] = t.obj.Uint8(OffsetAPVTx_ValidationRewardsOwnerAddress + i)
	}
	return threshold, locktime, addr
}

// DelegationRewardsOwner mirrors ValidationRewardsOwner for the delegation
// rewards owner stub.
func (t AddPermissionlessValidatorTx) DelegationRewardsOwner() (uint32, uint64, ids.ShortID) {
	threshold := t.obj.Uint32(OffsetAPVTx_DelegationRewardsOwnerThreshold)
	locktime := t.obj.Uint64(OffsetAPVTx_DelegationRewardsOwnerLocktime)
	var addr ids.ShortID
	for i := 0; i < ids.ShortIDLen; i++ {
		addr[i] = t.obj.Uint8(OffsetAPVTx_DelegationRewardsOwnerAddress + i)
	}
	return threshold, locktime, addr
}

// DelegationShares returns the delegation-shares percentage in units of
// reward.PercentDenominator.
func (t AddPermissionlessValidatorTx) DelegationShares() uint32 {
	return t.obj.Uint32(OffsetAPVTx_DelegationShares)
}

func (t AddPermissionlessValidatorTx) Bytes() []byte { return t.msg.Bytes() }
func (t AddPermissionlessValidatorTx) IsZero() bool  { return t.msg == nil }

func WrapAddPermissionlessValidatorTx(b []byte) (AddPermissionlessValidatorTx, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return AddPermissionlessValidatorTx{}, err
	}
	obj := msg.Root()
	if TxKind(obj.Uint8(OffsetTxKind)) != TxKindAddPermissionlessValidator {
		return AddPermissionlessValidatorTx{}, ErrWrongTxKind
	}
	return AddPermissionlessValidatorTx{msg: msg, obj: obj}, nil
}

// OwnerStub is the v3 single-address owner stub used by APV's two
// rewards-owner fields.
type OwnerStub struct {
	Threshold uint32
	Locktime  uint64
	Address   ids.ShortID
}

// AddPermissionlessValidatorTxInput is the constructor input.
type AddPermissionlessValidatorTxInput struct {
	NetworkID    uint32
	BlockchainID ids.ID
	Outs         []OutputListEntry
	Ins          []InputListEntry
	Credentials  []CredentialListEntry
	Memo         []byte

	NodeID               ids.NodeID
	StakeStart           uint64
	StakeEnd             uint64
	StakeWeight          uint64
	StakeAssetID         ids.ID
	StakeAmount          uint64
	BLSPublicKey         [bls.PublicKeyLen]byte
	BLSProofOfPossession [bls.SignatureLen]byte

	ValidationRewardsOwner OwnerStub
	DelegationRewardsOwner OwnerStub
	DelegationShares       uint32
}

func NewAddPermissionlessValidatorTx(in AddPermissionlessValidatorTxInput) AddPermissionlessValidatorTx {
	cap := zap.HeaderSize + 16 + SizeAddPermissionlessValidatorTx
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

	ob := b.StartObject(SizeAddPermissionlessValidatorTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindAddPermissionlessValidator))
	ob.SetUint32(OffsetAPVTx_NetworkID, in.NetworkID)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetAPVTx_BlockchainID+i, in.BlockchainID[i])
		ob.SetUint8(OffsetAPVTx_StakeAssetID+i, in.StakeAssetID[i])
	}
	ob.SetList(OffsetAPVTx_OutsList, outsOff, outsCount)
	ob.SetList(OffsetAPVTx_InsList, insOff, insCount)
	ob.SetList(OffsetAPVTx_CredsList, credsOff, credsCount)
	ob.SetList(OffsetAPVTx_SigIndicesArr, sigIdxArrOff, sigIdxArrCount)
	ob.SetList(OffsetAPVTx_SigArr, sigArrOff, sigArrCount)
	ob.SetBytes(OffsetAPVTx_Memo, in.Memo)
	for i := 0; i < ids.ShortIDLen; i++ {
		ob.SetUint8(OffsetAPVTx_NodeID+i, in.NodeID[i])
		ob.SetUint8(OffsetAPVTx_ValidationRewardsOwnerAddress+i, in.ValidationRewardsOwner.Address[i])
		ob.SetUint8(OffsetAPVTx_DelegationRewardsOwnerAddress+i, in.DelegationRewardsOwner.Address[i])
	}
	ob.SetUint64(OffsetAPVTx_StakeStart, in.StakeStart)
	ob.SetUint64(OffsetAPVTx_StakeEnd, in.StakeEnd)
	ob.SetUint64(OffsetAPVTx_StakeWeight, in.StakeWeight)
	ob.SetUint64(OffsetAPVTx_StakeAmount, in.StakeAmount)
	for i := 0; i < bls.PublicKeyLen; i++ {
		ob.SetUint8(OffsetAPVTx_BLSPublicKey+i, in.BLSPublicKey[i])
	}
	for i := 0; i < bls.SignatureLen; i++ {
		ob.SetUint8(OffsetAPVTx_BLSProofOfPossession+i, in.BLSProofOfPossession[i])
	}
	ob.SetUint32(OffsetAPVTx_ValidationRewardsOwnerThreshold, in.ValidationRewardsOwner.Threshold)
	ob.SetUint64(OffsetAPVTx_ValidationRewardsOwnerLocktime, in.ValidationRewardsOwner.Locktime)
	ob.SetUint32(OffsetAPVTx_DelegationRewardsOwnerThreshold, in.DelegationRewardsOwner.Threshold)
	ob.SetUint64(OffsetAPVTx_DelegationRewardsOwnerLocktime, in.DelegationRewardsOwner.Locktime)
	ob.SetUint32(OffsetAPVTx_DelegationShares, in.DelegationShares)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return AddPermissionlessValidatorTx{msg: msg, obj: msg.Root()}
}
