// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// CreateChainTx is the v3 create-blockchain envelope. It carries the
// BaseTxFull spending state, the parent network ID, the child VM ID, an
// optional genesis-data byte blob (variable; lives in the variable section),
// a single-address Owner stub, and the SHA-256 hash of the corresponding
// Warp message (when this CreateChain was originated via cross-network
// commit). The Warp message body itself is NOT embedded — only its hash —
// because the body lives in the signed Warp envelope outside this tx.
//
// Fixed-section layout (size 173 bytes):
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
//	ParentNetwork      32B    @ 85    (ids.ID; the parent network)
//	VMID               32B    @ 117   (ids.ID; the VM identifier)
//	OwnerThreshold     uint32 @ 149
//	OwnerLocktime      uint64 @ 153
//	OwnerAddress       20B    @ 161
//	WarpMessageHash    32B    @ 181   (sha256 of the originating Warp msg; zero if none)
//	GenesisData        bytes  @ 213   (offset+length; variable)
//	Size                      = 221
const (
	OffsetCreateChainTx_NetworkID       = 1
	OffsetCreateChainTx_BlockchainID    = 5
	OffsetCreateChainTx_OutsList        = 37
	OffsetCreateChainTx_InsList         = 45
	OffsetCreateChainTx_CredsList       = 53
	OffsetCreateChainTx_SigIndicesArr   = 61
	OffsetCreateChainTx_SigArr          = 69
	OffsetCreateChainTx_Memo            = 77
	OffsetCreateChainTx_ParentNetwork   = 85
	OffsetCreateChainTx_VMID            = 117
	OffsetCreateChainTx_OwnerThreshold  = 149
	OffsetCreateChainTx_OwnerLocktime   = 153
	OffsetCreateChainTx_OwnerAddress    = 161
	OffsetCreateChainTx_WarpMessageHash = OffsetCreateChainTx_OwnerAddress + ids.ShortIDLen
	OffsetCreateChainTx_GenesisData     = OffsetCreateChainTx_WarpMessageHash + 32
	SizeCreateChainTx                   = OffsetCreateChainTx_GenesisData + 8
)

// CreateChainTx is the zero-copy accessor.
type CreateChainTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t CreateChainTx) NetworkID() uint32 { return t.obj.Uint32(OffsetCreateChainTx_NetworkID) }

func (t CreateChainTx) BlockchainID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetCreateChainTx_BlockchainID + i)
	}
	return out
}

func (t CreateChainTx) ParentNetwork() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetCreateChainTx_ParentNetwork + i)
	}
	return out
}

func (t CreateChainTx) VMID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetCreateChainTx_VMID + i)
	}
	return out
}

func (t CreateChainTx) Owner() (uint32, uint64, ids.ShortID) {
	threshold := t.obj.Uint32(OffsetCreateChainTx_OwnerThreshold)
	locktime := t.obj.Uint64(OffsetCreateChainTx_OwnerLocktime)
	var addr ids.ShortID
	for i := 0; i < ids.ShortIDLen; i++ {
		addr[i] = t.obj.Uint8(OffsetCreateChainTx_OwnerAddress + i)
	}
	return threshold, locktime, addr
}

// WarpMessageHash returns the SHA-256 hash of the originating Warp message.
// Returns the zero ids.ID when this CreateChain was not originated via
// a cross-network commit.
func (t CreateChainTx) WarpMessageHash() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetCreateChainTx_WarpMessageHash + i)
	}
	return out
}

// GenesisData returns the chain's genesis bytes.
//
// READ-ONLY: see BaseTxFull.Memo for the aliasing contract.
func (t CreateChainTx) GenesisData() []byte {
	return t.obj.Bytes(OffsetCreateChainTx_GenesisData)
}

func (t CreateChainTx) Outs() OutputList { return OutputListView(t.obj, OffsetCreateChainTx_OutsList) }
func (t CreateChainTx) Ins() InputList   { return InputListView(t.obj, OffsetCreateChainTx_InsList) }
func (t CreateChainTx) Credentials() CredentialList {
	return CredentialListView(t.obj, OffsetCreateChainTx_CredsList)
}
func (t CreateChainTx) SigIndicesArray() SigIndicesArray {
	return SigIndicesArrayView(t.obj, OffsetCreateChainTx_SigIndicesArr)
}
func (t CreateChainTx) SignatureArray() SignatureArray {
	return SignatureArrayView(t.obj, OffsetCreateChainTx_SigArr)
}

func (t CreateChainTx) Memo() []byte { return t.obj.Bytes(OffsetCreateChainTx_Memo) }

func (t CreateChainTx) Bytes() []byte { return t.msg.Bytes() }
func (t CreateChainTx) IsZero() bool  { return t.msg == nil }

func WrapCreateChainTx(b []byte) (CreateChainTx, error) {
	msg, obj, err := parseAndCheckKind(b, TxKindCreateChain)
	if err != nil {
		return CreateChainTx{}, err
	}
	return CreateChainTx{msg: msg, obj: obj}, nil
}

// CreateChainTxInput is the constructor input.
type CreateChainTxInput struct {
	NetworkID       uint32
	BlockchainID    ids.ID
	Outs            []OutputListEntry
	Ins             []InputListEntry
	Credentials     []CredentialListEntry
	Memo            []byte
	ParentNetwork   ids.ID
	VMID            ids.ID
	Owner           OwnerStub
	WarpMessageHash ids.ID
	GenesisData     []byte
}

func NewCreateChainTx(in CreateChainTxInput) CreateChainTx {
	cap := zap.HeaderSize + 16 + SizeCreateChainTx
	cap += len(in.Outs) * SizeTransferableOutput
	cap += len(in.Ins) * SizeTransferableInput
	cap += len(in.Credentials) * SizeCredential
	cap += len(in.Memo) + len(in.GenesisData)
	b := zap.NewBuilder(cap)

	outsOff, outsCount := WriteOutputList(b, in.Outs)
	insOff, insCount, sigIndices := WriteInputList(b, in.Ins)
	credsOff, credsCount, sigBlobs := WriteCredentialList(b, in.Credentials)
	sigIdxArrOff, sigIdxArrCount := WriteSigIndicesArray(b, sigIndices)
	sigArrOff, sigArrCount := WriteSignatureArray(b, sigBlobs)

	ob := b.StartObject(SizeCreateChainTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindCreateChain))
	ob.SetUint32(OffsetCreateChainTx_NetworkID, in.NetworkID)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetCreateChainTx_BlockchainID+i, in.BlockchainID[i])
		ob.SetUint8(OffsetCreateChainTx_ParentNetwork+i, in.ParentNetwork[i])
		ob.SetUint8(OffsetCreateChainTx_VMID+i, in.VMID[i])
		ob.SetUint8(OffsetCreateChainTx_WarpMessageHash+i, in.WarpMessageHash[i])
	}
	ob.SetList(OffsetCreateChainTx_OutsList, outsOff, outsCount)
	ob.SetList(OffsetCreateChainTx_InsList, insOff, insCount)
	ob.SetList(OffsetCreateChainTx_CredsList, credsOff, credsCount)
	ob.SetList(OffsetCreateChainTx_SigIndicesArr, sigIdxArrOff, sigIdxArrCount)
	ob.SetList(OffsetCreateChainTx_SigArr, sigArrOff, sigArrCount)
	ob.SetBytes(OffsetCreateChainTx_Memo, in.Memo)
	ob.SetUint32(OffsetCreateChainTx_OwnerThreshold, in.Owner.Threshold)
	ob.SetUint64(OffsetCreateChainTx_OwnerLocktime, in.Owner.Locktime)
	for i := 0; i < ids.ShortIDLen; i++ {
		ob.SetUint8(OffsetCreateChainTx_OwnerAddress+i, in.Owner.Address[i])
	}
	ob.SetBytes(OffsetCreateChainTx_GenesisData, in.GenesisData)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return CreateChainTx{msg: msg, obj: msg.Root()}
}
