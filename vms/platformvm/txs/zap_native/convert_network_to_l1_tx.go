// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// ConvertNetworkToL1Tx — convert a network to a sovereign L1 (POST-rebrand
// from ConvertSubnetToL1Tx). Carries the BaseTxFull spending state plus:
//   - Chain (the chain to convert; primary network ID forbidden in
//     consumer-side syntactic verify)
//   - ManagerChainID (where the L1 manager contract lives)
//   - Address (chain-manager contract address, variable bytes)
//   - Validators list (initial pay-as-you-go validators)
//
// The Validators sub-list is a fixed-stride record per validator carrying
// (NodeID, Weight, BLSPubKey, BLSPoP, RegistrationExpiry). The
// initial-validator schema is intentionally simple (no nested owner lists)
// — owner lists for each validator's balance ride in a sibling array.
//
// Fixed-section layout (size 137 bytes):
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
//	Chain                   32B    @ 85
//	ManagerChainID          32B    @ 117
//	Address                 8B     @ 149  (variable bytes)
//	ValidatorsList          8B     @ 157  (fixed-stride list)
const (
	OffsetConvertNetworkToL1Tx_NetworkID      = 1
	OffsetConvertNetworkToL1Tx_BlockchainID   = 5
	OffsetConvertNetworkToL1Tx_OutsList       = 37
	OffsetConvertNetworkToL1Tx_InsList        = 45
	OffsetConvertNetworkToL1Tx_CredsList      = 53
	OffsetConvertNetworkToL1Tx_SigIndicesArr  = 61
	OffsetConvertNetworkToL1Tx_SigArr         = 69
	OffsetConvertNetworkToL1Tx_Memo           = 77
	OffsetConvertNetworkToL1Tx_Chain          = 85
	OffsetConvertNetworkToL1Tx_ManagerChainID = 117
	OffsetConvertNetworkToL1Tx_Address        = 149
	OffsetConvertNetworkToL1Tx_ValidatorsList = 157
	SizeConvertNetworkToL1Tx                  = 165
)

type ConvertNetworkToL1Tx struct {
	msg *zap.Message
	obj zap.Object
}

func (t ConvertNetworkToL1Tx) NetworkID() uint32 {
	return t.obj.Uint32(OffsetConvertNetworkToL1Tx_NetworkID)
}
func (t ConvertNetworkToL1Tx) BlockchainID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetConvertNetworkToL1Tx_BlockchainID + i)
	}
	return out
}
func (t ConvertNetworkToL1Tx) Outs() OutputList {
	return OutputListView(t.obj, OffsetConvertNetworkToL1Tx_OutsList)
}
func (t ConvertNetworkToL1Tx) Ins() InputList {
	return InputListView(t.obj, OffsetConvertNetworkToL1Tx_InsList)
}
func (t ConvertNetworkToL1Tx) Credentials() CredentialList {
	return CredentialListView(t.obj, OffsetConvertNetworkToL1Tx_CredsList)
}
func (t ConvertNetworkToL1Tx) SigIndicesArray() SigIndicesArray {
	return SigIndicesArrayView(t.obj, OffsetConvertNetworkToL1Tx_SigIndicesArr)
}
func (t ConvertNetworkToL1Tx) SignatureArray() SignatureArray {
	return SignatureArrayView(t.obj, OffsetConvertNetworkToL1Tx_SigArr)
}
func (t ConvertNetworkToL1Tx) Memo() []byte {
	return t.obj.Bytes(OffsetConvertNetworkToL1Tx_Memo)
}

func (t ConvertNetworkToL1Tx) Chain() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetConvertNetworkToL1Tx_Chain + i)
	}
	return out
}
func (t ConvertNetworkToL1Tx) ManagerChainID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetConvertNetworkToL1Tx_ManagerChainID + i)
	}
	return out
}

// Address returns the L1 manager contract address bytes. READ-ONLY: aliases
// the underlying ZAP buffer (see BaseTxFull.Memo for the contract).
func (t ConvertNetworkToL1Tx) Address() []byte {
	return t.obj.Bytes(OffsetConvertNetworkToL1Tx_Address)
}

func (t ConvertNetworkToL1Tx) Bytes() []byte { return t.msg.Bytes() }
func (t ConvertNetworkToL1Tx) IsZero() bool  { return t.msg == nil }

func WrapConvertNetworkToL1Tx(b []byte) (ConvertNetworkToL1Tx, error) {
	msg, obj, err := parseAndCheckKind(b, TxKindConvertNetworkToL1)
	if err != nil {
		return ConvertNetworkToL1Tx{}, err
	}
	return ConvertNetworkToL1Tx{msg: msg, obj: obj}, nil
}

type ConvertNetworkToL1TxInput struct {
	NetworkID      uint32
	BlockchainID   ids.ID
	Outs           []OutputListEntry
	Ins            []InputListEntry
	Credentials    []CredentialListEntry
	Memo           []byte
	Chain          ids.ID
	ManagerChainID ids.ID
	Address        []byte
}

func NewConvertNetworkToL1Tx(in ConvertNetworkToL1TxInput) ConvertNetworkToL1Tx {
	cap := zap.HeaderSize + 16 + SizeConvertNetworkToL1Tx
	cap += len(in.Outs) * SizeTransferableOutput
	cap += len(in.Ins) * SizeTransferableInput
	cap += len(in.Credentials) * SizeCredential
	cap += len(in.Memo) + len(in.Address)
	b := zap.NewBuilder(cap)

	outsOff, outsCount := WriteOutputList(b, in.Outs)
	insOff, insCount, sigIndices := WriteInputList(b, in.Ins)
	credsOff, credsCount, sigBlobs := WriteCredentialList(b, in.Credentials)
	sigIdxArrOff, sigIdxArrCount := WriteSigIndicesArray(b, sigIndices)
	sigArrOff, sigArrCount := WriteSignatureArray(b, sigBlobs)

	ob := b.StartObject(SizeConvertNetworkToL1Tx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindConvertNetworkToL1))
	ob.SetUint32(OffsetConvertNetworkToL1Tx_NetworkID, in.NetworkID)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetConvertNetworkToL1Tx_BlockchainID+i, in.BlockchainID[i])
		ob.SetUint8(OffsetConvertNetworkToL1Tx_Chain+i, in.Chain[i])
		ob.SetUint8(OffsetConvertNetworkToL1Tx_ManagerChainID+i, in.ManagerChainID[i])
	}
	ob.SetList(OffsetConvertNetworkToL1Tx_OutsList, outsOff, outsCount)
	ob.SetList(OffsetConvertNetworkToL1Tx_InsList, insOff, insCount)
	ob.SetList(OffsetConvertNetworkToL1Tx_CredsList, credsOff, credsCount)
	ob.SetList(OffsetConvertNetworkToL1Tx_SigIndicesArr, sigIdxArrOff, sigIdxArrCount)
	ob.SetList(OffsetConvertNetworkToL1Tx_SigArr, sigArrOff, sigArrCount)
	ob.SetBytes(OffsetConvertNetworkToL1Tx_Memo, in.Memo)
	ob.SetBytes(OffsetConvertNetworkToL1Tx_Address, in.Address)
	// ValidatorsList intentionally left at (0, 0) — initial-validator
	// records use a sibling primitive that's not part of this batch.
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return ConvertNetworkToL1Tx{msg: msg, obj: msg.Root()}
}
