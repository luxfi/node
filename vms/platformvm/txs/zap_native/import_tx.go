// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// ImportTx is the v3 cross-chain import: pull funds from a source chain via
// imported UTXOs. The Ins list carries BOTH local fee-paying inputs and the
// remote (imported) inputs in a single fixed-stride array; the
// ImportedInsStart / ImportedInsCount header fields identify the imported
// slice within. This keeps the SigIndicesArray / SignatureArray single
// shared arrays across all inputs without re-base bookkeeping. Local Ins
// occupy positions [0, ImportedInsStart); imported Ins occupy
// [ImportedInsStart, ImportedInsStart+ImportedInsCount).
//
// Fixed-section layout (size 93 bytes):
//
//	TxKind                 uint8  @ 0
//	NetworkID              uint32 @ 1
//	BlockchainID           32B    @ 5
//	OutsList               8B     @ 37
//	InsList                8B     @ 45   (combined: local + imported)
//	CredsList              8B     @ 53
//	SigIndicesArr          8B     @ 61
//	SigArr                 8B     @ 69
//	Memo                   8B     @ 77
//	SourceNetwork          32B    @ 85   (...wait, that's 117. Re-do.)
//
// Actually wait. 85 + 32 = 117. Plus ImportedInsStart/Count (8) = 125.
//
// Re-layout to keep TxKind@0 alignment and end size 125 bytes:
const (
	OffsetImportTx_NetworkID         = 1
	OffsetImportTx_BlockchainID      = 5
	OffsetImportTx_OutsList          = 37
	OffsetImportTx_InsList           = 45
	OffsetImportTx_CredsList         = 53
	OffsetImportTx_SigIndicesArr     = 61
	OffsetImportTx_SigArr            = 69
	OffsetImportTx_Memo              = 77
	OffsetImportTx_SourceNetwork     = 85
	OffsetImportTx_ImportedInsStart  = 117 // uint32 (index into Ins list)
	OffsetImportTx_ImportedInsCount  = 121 // uint32
	SizeImportTx                     = 125
)

// ImportTx is the zero-copy accessor for an v3 ImportTx.
type ImportTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t ImportTx) NetworkID() uint32 { return t.obj.Uint32(OffsetImportTx_NetworkID) }

func (t ImportTx) BlockchainID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetImportTx_BlockchainID + i)
	}
	return out
}

// SourceNetwork returns the network identifier the UTXOs are imported from.
func (t ImportTx) SourceNetwork() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetImportTx_SourceNetwork + i)
	}
	return out
}

func (t ImportTx) Outs() OutputList { return OutputListView(t.obj, OffsetImportTx_OutsList) }
func (t ImportTx) Ins() InputList   { return InputListView(t.obj, OffsetImportTx_InsList) }
func (t ImportTx) Credentials() CredentialList {
	return CredentialListView(t.obj, OffsetImportTx_CredsList)
}
func (t ImportTx) SigIndicesArray() SigIndicesArray {
	return SigIndicesArrayView(t.obj, OffsetImportTx_SigIndicesArr)
}
func (t ImportTx) SignatureArray() SignatureArray {
	return SignatureArrayView(t.obj, OffsetImportTx_SigArr)
}

// ImportedInsRange returns (start, count) marking the imported-input slice
// within the Ins list. Ins[0..start) are local fee-payers;
// Ins[start..start+count) are the imported (remote-source) inputs.
func (t ImportTx) ImportedInsRange() (uint32, uint32) {
	return t.obj.Uint32(OffsetImportTx_ImportedInsStart),
		t.obj.Uint32(OffsetImportTx_ImportedInsCount)
}

// Memo returns the memo bytes. READ-ONLY (see BaseTxFull.Memo).
func (t ImportTx) Memo() []byte { return t.obj.Bytes(OffsetImportTx_Memo) }

func (t ImportTx) Bytes() []byte { return t.msg.Bytes() }
func (t ImportTx) IsZero() bool  { return t.msg == nil }

func WrapImportTx(b []byte) (ImportTx, error) {
	msg, obj, err := parseAndCheckKind(b, TxKindImport)
	if err != nil {
		return ImportTx{}, err
	}
	return ImportTx{msg: msg, obj: obj}, nil
}

// ImportTxInput is the constructor input for NewImportTx. LocalIns and
// ImportedIns are written as a single combined list; the constructor
// records ImportedInsStart = len(LocalIns).
type ImportTxInput struct {
	NetworkID     uint32
	BlockchainID  ids.ID
	SourceNetwork ids.ID
	Outs          []OutputListEntry
	LocalIns      []InputListEntry
	ImportedIns   []InputListEntry
	Credentials   []CredentialListEntry
	Memo          []byte
}

func NewImportTx(in ImportTxInput) ImportTx {
	cap := zap.HeaderSize + 16 + SizeImportTx
	cap += len(in.Outs) * SizeTransferableOutput
	cap += (len(in.LocalIns) + len(in.ImportedIns)) * SizeTransferableInput
	cap += len(in.Credentials) * SizeCredential
	cap += len(in.Memo)
	b := zap.NewBuilder(cap)

	combinedIns := make([]InputListEntry, 0, len(in.LocalIns)+len(in.ImportedIns))
	combinedIns = append(combinedIns, in.LocalIns...)
	combinedIns = append(combinedIns, in.ImportedIns...)

	outsOff, outsCount := WriteOutputList(b, in.Outs)
	insOff, insCount, sigIndices := WriteInputList(b, combinedIns)
	credsOff, credsCount, sigBlobs := WriteCredentialList(b, in.Credentials)
	sigIdxArrOff, sigIdxArrCount := WriteSigIndicesArray(b, sigIndices)
	sigArrOff, sigArrCount := WriteSignatureArray(b, sigBlobs)

	ob := b.StartObject(SizeImportTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindImport))
	ob.SetUint32(OffsetImportTx_NetworkID, in.NetworkID)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetImportTx_BlockchainID+i, in.BlockchainID[i])
		ob.SetUint8(OffsetImportTx_SourceNetwork+i, in.SourceNetwork[i])
	}
	ob.SetList(OffsetImportTx_OutsList, outsOff, outsCount)
	ob.SetList(OffsetImportTx_InsList, insOff, insCount)
	ob.SetList(OffsetImportTx_CredsList, credsOff, credsCount)
	ob.SetList(OffsetImportTx_SigIndicesArr, sigIdxArrOff, sigIdxArrCount)
	ob.SetList(OffsetImportTx_SigArr, sigArrOff, sigArrCount)
	ob.SetBytes(OffsetImportTx_Memo, in.Memo)
	ob.SetUint32(OffsetImportTx_ImportedInsStart, uint32(len(in.LocalIns)))
	ob.SetUint32(OffsetImportTx_ImportedInsCount, uint32(len(in.ImportedIns)))
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return ImportTx{msg: msg, obj: msg.Root()}
}
