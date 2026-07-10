// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Batch 2 delta bridges: types whose delta carries extra spending lists
// (Import/Export), variable bytes/strings/id-lists (CreateChain), or fixed
// crypto material (RegisterL1Validator BLS PoP). Plus the shared extra-list /
// validator / signer / id-list encoders reused by batch 3 (the Add* family).

import (
	"github.com/luxfi/ids"
	zn "github.com/luxfi/proto/zap_native"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

const (
	blsPublicKeyLen = 48 // bls.PublicKeyLen
	blsSignatureLen = 96 // bls.SignatureLen
)

// ---- extra spending-list helpers (a second Outs/Ins beyond the envelope) ----

func writeExtraOuts(b *zap.Builder, outs []*lux.TransferableOutput) (listOff, listCount, addrOff, addrCount int, err error) {
	entries, err := toOutputEntries(outs)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	listOff, listCount, addrs := zn.WriteOutputFullList(b, entries)
	addrOff, addrCount = zn.WriteOwnerAddrArray(b, addrs)
	return listOff, listCount, addrOff, addrCount, nil
}

func readExtraOuts(obj zap.Object, listPtrOff, addrPtrOff int) []*lux.TransferableOutput {
	return fromOutputEntries(zn.OutputFullListView(obj, listPtrOff), zn.OwnerAddrArrayView(obj, addrPtrOff))
}

func writeExtraIns(b *zap.Builder, ins []*lux.TransferableInput) (listOff, listCount, sigOff, sigCount int, err error) {
	entries, err := toInputEntries(ins)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	listOff, listCount, sigIdx := zn.WriteInputFullList(b, entries)
	sigOff, sigCount = zn.WriteSigIndicesArray(b, sigIdx)
	return listOff, listCount, sigOff, sigCount, nil
}

func readExtraIns(obj zap.Object, listPtrOff, sigPtrOff int) []*lux.TransferableInput {
	return fromInputEntries(zn.InputFullListView(obj, listPtrOff), zn.SigIndicesArrayView(obj, sigPtrOff))
}

// ---- id-list helper ([]ids.ID) ----

func writeIDList(b *zap.Builder, list []ids.ID) (off, count int) {
	if len(list) == 0 {
		return 0, 0
	}
	lb := b.StartList(32)
	for _, id := range list {
		lb.AddBytes(id[:])
	}
	off, _ = lb.Finish()
	return off, len(list)
}

func readIDList(obj zap.Object, ptrOff int) []ids.ID {
	l := obj.ListStride(ptrOff, 32)
	n := l.Len()
	if n == 0 {
		return nil
	}
	out := make([]ids.ID, n)
	for i := 0; i < n; i++ {
		o := l.Object(i, 32)
		for j := 0; j < 32; j++ {
			out[i][j] = o.Uint8(j)
		}
	}
	return out
}

// =====================================================================
// TransferChainOwnershipTx: envelope + Chain + ChainAuth + Owner
// =====================================================================

const (
	OffsetTCO_Chain          = SpendEnvelopeSize      // 32B
	OffsetTCO_AuthPtr        = SpendEnvelopeSize + 32 // sig-idx list ptr (8B)
	OffsetTCO_OwnerThreshold = SpendEnvelopeSize + 40 // u32
	OffsetTCO_OwnerLocktime  = SpendEnvelopeSize + 44 // u64
	OffsetTCO_OwnerAddrPtr   = SpendEnvelopeSize + 52 // list ptr (8B)
	SizeTCOTx                = SpendEnvelopeSize + 60
)

func marshalTransferChainOwnershipTx(tx *TransferChainOwnershipTx) ([]byte, error) {
	outs, err := toOutputEntries(tx.Outs)
	if err != nil {
		return nil, err
	}
	ins, err := toInputEntries(tx.Ins)
	if err != nil {
		return nil, err
	}
	b := newBuilderFor(SizeTCOTx, &tx.BaseTx)
	so := writeSpendingLists(b, outs, ins)
	authOff, authCount, err := writeAuthDelta(b, tx.ChainAuth)
	if err != nil {
		return nil, err
	}
	ow, err := writeOwnerDelta(b, tx.Owner)
	if err != nil {
		return nil, err
	}
	ob := b.StartObject(SizeTCOTx)
	setSpendingEnvelope(ob, zn.TxKindTransferChainOwnership, tx.NetworkID, tx.BlockchainID, so, tx.Memo)
	setID(ob, OffsetTCO_Chain, tx.Chain)
	ob.SetList(OffsetTCO_AuthPtr, authOff, authCount)
	setOwnerDelta(ob, OffsetTCO_OwnerThreshold, OffsetTCO_OwnerLocktime, OffsetTCO_OwnerAddrPtr, ow)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func unmarshalTransferChainOwnershipTx(buf []byte) (UnsignedTx, error) {
	msg, err := zap.Parse(buf)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	tx := &TransferChainOwnershipTx{
		BaseTx:    BaseTx{BaseTx: readSpending(obj)},
		Chain:     readID(obj, OffsetTCO_Chain),
		ChainAuth: readAuthDelta(obj, OffsetTCO_AuthPtr),
		Owner:     readOwnerDelta(obj, OffsetTCO_OwnerThreshold, OffsetTCO_OwnerLocktime, OffsetTCO_OwnerAddrPtr),
	}
	tx.SetBytes(buf)
	return tx, nil
}

// =====================================================================
// RegisterL1ValidatorTx: envelope + Balance + PoP[96] + Message
// =====================================================================

const (
	OffsetRegL1_Balance    = SpendEnvelopeSize                           // u64
	OffsetRegL1_PoP        = SpendEnvelopeSize + 8                       // 96B fixed
	OffsetRegL1_MessagePtr = SpendEnvelopeSize + 8 + blsSignatureLen     // bytes ptr (8B)
	SizeRegL1Tx            = SpendEnvelopeSize + 8 + blsSignatureLen + 8 // = 189
)

func marshalRegisterL1ValidatorTx(tx *RegisterL1ValidatorTx) ([]byte, error) {
	outs, err := toOutputEntries(tx.Outs)
	if err != nil {
		return nil, err
	}
	ins, err := toInputEntries(tx.Ins)
	if err != nil {
		return nil, err
	}
	b := newBuilderFor(SizeRegL1Tx+len(tx.Message), &tx.BaseTx)
	so := writeSpendingLists(b, outs, ins)
	ob := b.StartObject(SizeRegL1Tx)
	setSpendingEnvelope(ob, zn.TxKindRegisterL1Validator, tx.NetworkID, tx.BlockchainID, so, tx.Memo)
	ob.SetUint64(OffsetRegL1_Balance, tx.Balance)
	ob.SetBytesFixed(OffsetRegL1_PoP, tx.ProofOfPossession[:])
	ob.SetBytes(OffsetRegL1_MessagePtr, tx.Message)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func unmarshalRegisterL1ValidatorTx(buf []byte) (UnsignedTx, error) {
	msg, err := zap.Parse(buf)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	tx := &RegisterL1ValidatorTx{
		BaseTx:  BaseTx{BaseTx: readSpending(obj)},
		Balance: obj.Uint64(OffsetRegL1_Balance),
	}
	for i := 0; i < blsSignatureLen; i++ {
		tx.ProofOfPossession[i] = obj.Uint8(OffsetRegL1_PoP + i)
	}
	if m := obj.Bytes(OffsetRegL1_MessagePtr); len(m) > 0 {
		tx.Message = append([]byte(nil), m...)
	}
	tx.SetBytes(buf)
	return tx, nil
}

// =====================================================================
// ImportTx: envelope + SourceChain + ImportedInputs (extra input list)
// =====================================================================

const (
	OffsetImport_SourceChain = SpendEnvelopeSize      // 32B
	OffsetImport_InsPtr      = SpendEnvelopeSize + 32 // input list ptr (8B)
	OffsetImport_SigIdxPtr   = SpendEnvelopeSize + 40 // shared sig-idx array ptr (8B)
	SizeImportTx             = SpendEnvelopeSize + 48
)

func marshalImportTx(tx *ImportTx) ([]byte, error) {
	outs, err := toOutputEntries(tx.Outs)
	if err != nil {
		return nil, err
	}
	ins, err := toInputEntries(tx.Ins)
	if err != nil {
		return nil, err
	}
	b := newBuilderFor(SizeImportTx+len(tx.ImportedInputs)*zn.SizeTransferableInputFull, &tx.BaseTx)
	so := writeSpendingLists(b, outs, ins)
	impOff, impCount, sigOff, sigCount, err := writeExtraIns(b, tx.ImportedInputs)
	if err != nil {
		return nil, err
	}
	ob := b.StartObject(SizeImportTx)
	setSpendingEnvelope(ob, zn.TxKindImport, tx.NetworkID, tx.BlockchainID, so, tx.Memo)
	setID(ob, OffsetImport_SourceChain, tx.SourceChain)
	ob.SetList(OffsetImport_InsPtr, impOff, impCount)
	ob.SetList(OffsetImport_SigIdxPtr, sigOff, sigCount)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func unmarshalImportTx(buf []byte) (UnsignedTx, error) {
	msg, err := zap.Parse(buf)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	tx := &ImportTx{
		BaseTx:         BaseTx{BaseTx: readSpending(obj)},
		SourceChain:    readID(obj, OffsetImport_SourceChain),
		ImportedInputs: readExtraIns(obj, OffsetImport_InsPtr, OffsetImport_SigIdxPtr),
	}
	tx.SetBytes(buf)
	return tx, nil
}

// =====================================================================
// ExportTx: envelope + DestinationChain + ExportedOutputs (extra output list)
// =====================================================================

const (
	OffsetExport_DestChain = SpendEnvelopeSize      // 32B
	OffsetExport_OutsPtr   = SpendEnvelopeSize + 32 // output list ptr (8B)
	OffsetExport_AddrPtr   = SpendEnvelopeSize + 40 // shared owner-addr array ptr (8B)
	SizeExportTx           = SpendEnvelopeSize + 48
)

func marshalExportTx(tx *ExportTx) ([]byte, error) {
	outs, err := toOutputEntries(tx.Outs)
	if err != nil {
		return nil, err
	}
	ins, err := toInputEntries(tx.Ins)
	if err != nil {
		return nil, err
	}
	b := newBuilderFor(SizeExportTx+len(tx.ExportedOutputs)*zn.SizeTransferableOutputFull, &tx.BaseTx)
	so := writeSpendingLists(b, outs, ins)
	expOff, expCount, addrOff, addrCount, err := writeExtraOuts(b, tx.ExportedOutputs)
	if err != nil {
		return nil, err
	}
	ob := b.StartObject(SizeExportTx)
	setSpendingEnvelope(ob, zn.TxKindExport, tx.NetworkID, tx.BlockchainID, so, tx.Memo)
	setID(ob, OffsetExport_DestChain, tx.DestinationChain)
	ob.SetList(OffsetExport_OutsPtr, expOff, expCount)
	ob.SetList(OffsetExport_AddrPtr, addrOff, addrCount)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func unmarshalExportTx(buf []byte) (UnsignedTx, error) {
	msg, err := zap.Parse(buf)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	tx := &ExportTx{
		BaseTx:           BaseTx{BaseTx: readSpending(obj)},
		DestinationChain: readID(obj, OffsetExport_DestChain),
		ExportedOutputs:  readExtraOuts(obj, OffsetExport_OutsPtr, OffsetExport_AddrPtr),
	}
	tx.SetBytes(buf)
	return tx, nil
}

// =====================================================================
// CreateChainTx: envelope + ChainID + VMID + Name + FxIDs + Genesis + Auth
// =====================================================================

const (
	OffsetCreateChain_ChainID    = SpendEnvelopeSize      // 32B
	OffsetCreateChain_VMID       = SpendEnvelopeSize + 32 // 32B
	OffsetCreateChain_NamePtr    = SpendEnvelopeSize + 64 // bytes ptr (8B)
	OffsetCreateChain_FxIDsPtr   = SpendEnvelopeSize + 72 // id list ptr (8B)
	OffsetCreateChain_GenesisPtr = SpendEnvelopeSize + 80 // bytes ptr (8B)
	OffsetCreateChain_AuthPtr    = SpendEnvelopeSize + 88 // sig-idx list ptr (8B)
	SizeCreateChainTx            = SpendEnvelopeSize + 96
)

func marshalCreateChainTx(tx *CreateChainTx) ([]byte, error) {
	outs, err := toOutputEntries(tx.Outs)
	if err != nil {
		return nil, err
	}
	ins, err := toInputEntries(tx.Ins)
	if err != nil {
		return nil, err
	}
	b := newBuilderFor(SizeCreateChainTx+len(tx.BlockchainName)+len(tx.GenesisData)+len(tx.FxIDs)*32, &tx.BaseTx)
	so := writeSpendingLists(b, outs, ins)
	fxOff, fxCount := writeIDList(b, tx.FxIDs)
	authOff, authCount, err := writeAuthDelta(b, tx.ChainAuth)
	if err != nil {
		return nil, err
	}
	ob := b.StartObject(SizeCreateChainTx)
	setSpendingEnvelope(ob, zn.TxKindCreateChain, tx.NetworkID, tx.BlockchainID, so, tx.Memo)
	setID(ob, OffsetCreateChain_ChainID, tx.ChainID)
	setID(ob, OffsetCreateChain_VMID, tx.VMID)
	ob.SetText(OffsetCreateChain_NamePtr, tx.BlockchainName)
	ob.SetList(OffsetCreateChain_FxIDsPtr, fxOff, fxCount)
	ob.SetBytes(OffsetCreateChain_GenesisPtr, tx.GenesisData)
	ob.SetList(OffsetCreateChain_AuthPtr, authOff, authCount)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func unmarshalCreateChainTx(buf []byte) (UnsignedTx, error) {
	msg, err := zap.Parse(buf)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	tx := &CreateChainTx{
		BaseTx:         BaseTx{BaseTx: readSpending(obj)},
		ChainID:        readID(obj, OffsetCreateChain_ChainID),
		VMID:           readID(obj, OffsetCreateChain_VMID),
		BlockchainName: obj.Text(OffsetCreateChain_NamePtr),
		FxIDs:          readIDList(obj, OffsetCreateChain_FxIDsPtr),
		ChainAuth:      readAuthDelta(obj, OffsetCreateChain_AuthPtr),
	}
	if g := obj.Bytes(OffsetCreateChain_GenesisPtr); len(g) > 0 {
		tx.GenesisData = append([]byte(nil), g...)
	}
	tx.SetBytes(buf)
	return tx, nil
}
