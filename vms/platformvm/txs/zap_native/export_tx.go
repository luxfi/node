// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// ExportTx is the v3 cross-chain export: send funds from this chain to a
// destination chain. The Outs list carries BOTH local change outputs and the
// exported outputs to the destination chain in a single fixed-stride array;
// ExportedOutsStart / ExportedOutsCount mark the exported slice within.
// Local Outs occupy positions [0, ExportedOutsStart); exported Outs occupy
// [ExportedOutsStart, ExportedOutsStart+ExportedOutsCount).
//
// Fixed-section layout (size 125 bytes):
//
//	TxKind                  uint8  @ 0
//	NetworkID               uint32 @ 1
//	BlockchainID            32B    @ 5
//	OutsList                8B     @ 37  (combined: local change + exported)
//	InsList                 8B     @ 45  (local fee + funding inputs)
//	CredsList               8B     @ 53
//	SigIndicesArr           8B     @ 61
//	SigArr                  8B     @ 69
//	Memo                    8B     @ 77
//	DestinationNetwork      32B    @ 85  (chain the exports flow to)
//	ExportedOutsStart       uint32 @ 117 (index into Outs list)
//	ExportedOutsCount       uint32 @ 121
const (
	OffsetExportTx_NetworkID          = 1
	OffsetExportTx_BlockchainID       = 5
	OffsetExportTx_OutsList           = 37
	OffsetExportTx_InsList            = 45
	OffsetExportTx_CredsList          = 53
	OffsetExportTx_SigIndicesArr      = 61
	OffsetExportTx_SigArr             = 69
	OffsetExportTx_Memo               = 77
	OffsetExportTx_DestinationNetwork = 85
	OffsetExportTx_ExportedOutsStart  = 117
	OffsetExportTx_ExportedOutsCount  = 121
	SizeExportTx                      = 125
)

// ExportTx is the zero-copy accessor for a v3 ExportTx.
type ExportTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t ExportTx) NetworkID() uint32 { return t.obj.Uint32(OffsetExportTx_NetworkID) }

func (t ExportTx) BlockchainID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetExportTx_BlockchainID + i)
	}
	return out
}

// DestinationNetwork returns the network identifier the exports flow to.
func (t ExportTx) DestinationNetwork() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetExportTx_DestinationNetwork + i)
	}
	return out
}

func (t ExportTx) Outs() OutputList { return OutputListView(t.obj, OffsetExportTx_OutsList) }
func (t ExportTx) Ins() InputList   { return InputListView(t.obj, OffsetExportTx_InsList) }
func (t ExportTx) Credentials() CredentialList {
	return CredentialListView(t.obj, OffsetExportTx_CredsList)
}
func (t ExportTx) SigIndicesArray() SigIndicesArray {
	return SigIndicesArrayView(t.obj, OffsetExportTx_SigIndicesArr)
}
func (t ExportTx) SignatureArray() SignatureArray {
	return SignatureArrayView(t.obj, OffsetExportTx_SigArr)
}

// ExportedOutsRange returns (start, count) marking the exported-output slice
// within the Outs list. Outs[0..start) are local change;
// Outs[start..start+count) are the exported outputs.
func (t ExportTx) ExportedOutsRange() (uint32, uint32) {
	return t.obj.Uint32(OffsetExportTx_ExportedOutsStart),
		t.obj.Uint32(OffsetExportTx_ExportedOutsCount)
}

// Memo returns the memo bytes. READ-ONLY (see BaseTxFull.Memo).
func (t ExportTx) Memo() []byte { return t.obj.Bytes(OffsetExportTx_Memo) }

func (t ExportTx) Bytes() []byte { return t.msg.Bytes() }
func (t ExportTx) IsZero() bool  { return t.msg == nil }

func WrapExportTx(b []byte) (ExportTx, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return ExportTx{}, err
	}
	obj := msg.Root()
	if TxKind(obj.Uint8(OffsetTxKind)) != TxKindExport {
		return ExportTx{}, ErrWrongTxKind
	}
	return ExportTx{msg: msg, obj: obj}, nil
}

// ExportTxInput is the constructor input for NewExportTx.
type ExportTxInput struct {
	NetworkID          uint32
	BlockchainID       ids.ID
	DestinationNetwork ids.ID
	LocalOuts          []OutputListEntry
	ExportedOuts       []OutputListEntry
	Ins                []InputListEntry
	Credentials        []CredentialListEntry
	Memo               []byte
}

func NewExportTx(in ExportTxInput) ExportTx {
	cap := zap.HeaderSize + 16 + SizeExportTx
	cap += (len(in.LocalOuts) + len(in.ExportedOuts)) * SizeTransferableOutput
	cap += len(in.Ins) * SizeTransferableInput
	cap += len(in.Credentials) * SizeCredential
	cap += len(in.Memo)
	b := zap.NewBuilder(cap)

	combinedOuts := make([]OutputListEntry, 0, len(in.LocalOuts)+len(in.ExportedOuts))
	combinedOuts = append(combinedOuts, in.LocalOuts...)
	combinedOuts = append(combinedOuts, in.ExportedOuts...)

	outsOff, outsCount := WriteOutputList(b, combinedOuts)
	insOff, insCount, sigIndices := WriteInputList(b, in.Ins)
	credsOff, credsCount, sigBlobs := WriteCredentialList(b, in.Credentials)
	sigIdxArrOff, sigIdxArrCount := WriteSigIndicesArray(b, sigIndices)
	sigArrOff, sigArrCount := WriteSignatureArray(b, sigBlobs)

	ob := b.StartObject(SizeExportTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindExport))
	ob.SetUint32(OffsetExportTx_NetworkID, in.NetworkID)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetExportTx_BlockchainID+i, in.BlockchainID[i])
		ob.SetUint8(OffsetExportTx_DestinationNetwork+i, in.DestinationNetwork[i])
	}
	ob.SetList(OffsetExportTx_OutsList, outsOff, outsCount)
	ob.SetList(OffsetExportTx_InsList, insOff, insCount)
	ob.SetList(OffsetExportTx_CredsList, credsOff, credsCount)
	ob.SetList(OffsetExportTx_SigIndicesArr, sigIdxArrOff, sigIdxArrCount)
	ob.SetList(OffsetExportTx_SigArr, sigArrOff, sigArrCount)
	ob.SetBytes(OffsetExportTx_Memo, in.Memo)
	ob.SetUint32(OffsetExportTx_ExportedOutsStart, uint32(len(in.LocalOuts)))
	ob.SetUint32(OffsetExportTx_ExportedOutsCount, uint32(len(in.ExportedOuts)))
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return ExportTx{msg: msg, obj: msg.Root()}
}
