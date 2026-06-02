// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// CreateSovereignL1Tx atomically registers a sovereign L1 on P-chain.
// Combines CreateNetwork + AddChainValidator(*N) + CreateChain(*K) +
// ConvertNetworkToL1 into one atomic commit. See legacy
// /vms/platformvm/txs/create_sovereign_l1_tx.go for full semantics.
//
// This v3 form pins the most common single-chain L1 case (e.g. EVM-only).
// Multi-chain L1s (EVM + DEX + FHE) still ride the legacy schema until
// the per-chain Chains list primitive ships in a follow-up batch.
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
//	OwnerThreshold          uint32 @ 85
//	OwnerLocktime           uint64 @ 89
//	OwnerAddress            20B    @ 97
//	ManagerChainIdx         uint32 @ 117
//	ManagerAddress          8B     @ 121  (variable bytes)
//	ValidatorsList          8B     @ 129  (fixed-stride list; pending)
//	ChainsList              8B     @ 137  (fixed-stride list; pending)
//	VMID                    32B    @ 145  (single-chain v3 stub; multi-chain via legacy)
const (
	OffsetCreateSovereignL1Tx_NetworkID       = 1
	OffsetCreateSovereignL1Tx_BlockchainID    = 5
	OffsetCreateSovereignL1Tx_OutsList        = 37
	OffsetCreateSovereignL1Tx_InsList         = 45
	OffsetCreateSovereignL1Tx_CredsList       = 53
	OffsetCreateSovereignL1Tx_SigIndicesArr   = 61
	OffsetCreateSovereignL1Tx_SigArr          = 69
	OffsetCreateSovereignL1Tx_Memo            = 77
	OffsetCreateSovereignL1Tx_OwnerThreshold  = 85
	OffsetCreateSovereignL1Tx_OwnerLocktime   = 89
	OffsetCreateSovereignL1Tx_OwnerAddress    = 97
	OffsetCreateSovereignL1Tx_ManagerChainIdx = 117
	OffsetCreateSovereignL1Tx_ManagerAddress  = 121
	OffsetCreateSovereignL1Tx_ValidatorsList  = 129
	OffsetCreateSovereignL1Tx_ChainsList      = 137
	OffsetCreateSovereignL1Tx_VMID            = 145
	SizeCreateSovereignL1Tx                   = 177
)

type CreateSovereignL1Tx struct {
	msg *zap.Message
	obj zap.Object
}

func (t CreateSovereignL1Tx) NetworkID() uint32 {
	return t.obj.Uint32(OffsetCreateSovereignL1Tx_NetworkID)
}
func (t CreateSovereignL1Tx) BlockchainID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetCreateSovereignL1Tx_BlockchainID + i)
	}
	return out
}
func (t CreateSovereignL1Tx) Outs() OutputList {
	return OutputListView(t.obj, OffsetCreateSovereignL1Tx_OutsList)
}
func (t CreateSovereignL1Tx) Ins() InputList {
	return InputListView(t.obj, OffsetCreateSovereignL1Tx_InsList)
}
func (t CreateSovereignL1Tx) Credentials() CredentialList {
	return CredentialListView(t.obj, OffsetCreateSovereignL1Tx_CredsList)
}
func (t CreateSovereignL1Tx) SigIndicesArray() SigIndicesArray {
	return SigIndicesArrayView(t.obj, OffsetCreateSovereignL1Tx_SigIndicesArr)
}
func (t CreateSovereignL1Tx) SignatureArray() SignatureArray {
	return SignatureArrayView(t.obj, OffsetCreateSovereignL1Tx_SigArr)
}
func (t CreateSovereignL1Tx) Memo() []byte {
	return t.obj.Bytes(OffsetCreateSovereignL1Tx_Memo)
}

func (t CreateSovereignL1Tx) Owner() (uint32, uint64, ids.ShortID) {
	threshold := t.obj.Uint32(OffsetCreateSovereignL1Tx_OwnerThreshold)
	locktime := t.obj.Uint64(OffsetCreateSovereignL1Tx_OwnerLocktime)
	var addr ids.ShortID
	for i := 0; i < ids.ShortIDLen; i++ {
		addr[i] = t.obj.Uint8(OffsetCreateSovereignL1Tx_OwnerAddress + i)
	}
	return threshold, locktime, addr
}
func (t CreateSovereignL1Tx) ManagerChainIdx() uint32 {
	return t.obj.Uint32(OffsetCreateSovereignL1Tx_ManagerChainIdx)
}
func (t CreateSovereignL1Tx) ManagerAddress() []byte {
	return t.obj.Bytes(OffsetCreateSovereignL1Tx_ManagerAddress)
}
func (t CreateSovereignL1Tx) VMID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetCreateSovereignL1Tx_VMID + i)
	}
	return out
}

func (t CreateSovereignL1Tx) Bytes() []byte { return t.msg.Bytes() }
func (t CreateSovereignL1Tx) IsZero() bool  { return t.msg == nil }

func WrapCreateSovereignL1Tx(b []byte) (CreateSovereignL1Tx, error) {
	msg, obj, err := parseAndCheckKind(b, TxKindCreateSovereignL1)
	if err != nil {
		return CreateSovereignL1Tx{}, err
	}
	return CreateSovereignL1Tx{msg: msg, obj: obj}, nil
}

type CreateSovereignL1TxInput struct {
	NetworkID       uint32
	BlockchainID    ids.ID
	Outs            []OutputListEntry
	Ins             []InputListEntry
	Credentials     []CredentialListEntry
	Memo            []byte
	Owner           OwnerStub
	ManagerChainIdx uint32
	ManagerAddress  []byte
	VMID            ids.ID
}

func NewCreateSovereignL1Tx(in CreateSovereignL1TxInput) CreateSovereignL1Tx {
	cap := zap.HeaderSize + 16 + SizeCreateSovereignL1Tx
	cap += len(in.Outs) * SizeTransferableOutput
	cap += len(in.Ins) * SizeTransferableInput
	cap += len(in.Credentials) * SizeCredential
	cap += len(in.Memo) + len(in.ManagerAddress)
	b := zap.NewBuilder(cap)

	outsOff, outsCount := WriteOutputList(b, in.Outs)
	insOff, insCount, sigIndices := WriteInputList(b, in.Ins)
	credsOff, credsCount, sigBlobs := WriteCredentialList(b, in.Credentials)
	sigIdxArrOff, sigIdxArrCount := WriteSigIndicesArray(b, sigIndices)
	sigArrOff, sigArrCount := WriteSignatureArray(b, sigBlobs)

	ob := b.StartObject(SizeCreateSovereignL1Tx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindCreateSovereignL1))
	ob.SetUint32(OffsetCreateSovereignL1Tx_NetworkID, in.NetworkID)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetCreateSovereignL1Tx_BlockchainID+i, in.BlockchainID[i])
		ob.SetUint8(OffsetCreateSovereignL1Tx_VMID+i, in.VMID[i])
	}
	ob.SetList(OffsetCreateSovereignL1Tx_OutsList, outsOff, outsCount)
	ob.SetList(OffsetCreateSovereignL1Tx_InsList, insOff, insCount)
	ob.SetList(OffsetCreateSovereignL1Tx_CredsList, credsOff, credsCount)
	ob.SetList(OffsetCreateSovereignL1Tx_SigIndicesArr, sigIdxArrOff, sigIdxArrCount)
	ob.SetList(OffsetCreateSovereignL1Tx_SigArr, sigArrOff, sigArrCount)
	ob.SetBytes(OffsetCreateSovereignL1Tx_Memo, in.Memo)
	ob.SetUint32(OffsetCreateSovereignL1Tx_OwnerThreshold, in.Owner.Threshold)
	ob.SetUint64(OffsetCreateSovereignL1Tx_OwnerLocktime, in.Owner.Locktime)
	for i := 0; i < ids.ShortIDLen; i++ {
		ob.SetUint8(OffsetCreateSovereignL1Tx_OwnerAddress+i, in.Owner.Address[i])
	}
	ob.SetUint32(OffsetCreateSovereignL1Tx_ManagerChainIdx, in.ManagerChainIdx)
	ob.SetBytes(OffsetCreateSovereignL1Tx_ManagerAddress, in.ManagerAddress)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return CreateSovereignL1Tx{msg: msg, obj: msg.Root()}
}
