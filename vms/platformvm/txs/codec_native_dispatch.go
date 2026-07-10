// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Per-type native-ZAP bridges. Each arm maps a Go tx struct to its
// zap_native constructor (build) and accessor wrapper (parse). New arms are
// added type-by-type; when every type registered by codec.go is covered
// here, the package-level Codec flips to nativeManager and the reflection
// stack is deleted.
//
// The bridge is intentionally mechanical: build reads struct fields and
// writes them through the zap_native New* constructor; parse reads them
// back through the Wrap* accessor. There is no reflection on either side.

import (
	"fmt"

	"github.com/luxfi/node/vms/components/verify"
	zn "github.com/luxfi/proto/zap_native"
	"github.com/luxfi/zap"
)

// marshalUnsignedNative encodes a concrete UnsignedTx to its native-ZAP
// buffer. The returned bytes are a complete, self-delimiting ZAP message.
func marshalUnsignedNative(u UnsignedTx) ([]byte, error) {
	switch t := u.(type) {
	case *AdvanceTimeTx:
		return zn.NewAdvanceTimeTx(t.Time).Bytes(), nil
	case *RewardValidatorTx:
		return zn.NewRewardValidatorTx(t.TxID).Bytes(), nil
	case *BaseTx:
		return marshalBaseTx(t)
	case *IncreaseL1ValidatorBalanceTx:
		return marshalIncreaseL1ValidatorBalanceTx(t)
	case *SetL1ValidatorWeightTx:
		return marshalSetL1ValidatorWeightTx(t)
	case *DisableL1ValidatorTx:
		return marshalDisableL1ValidatorTx(t)
	case *RemoveChainValidatorTx:
		return marshalRemoveChainValidatorTx(t)
	case *CreateNetworkTx:
		return marshalCreateNetworkTx(t)
	case *TransferChainOwnershipTx:
		return marshalTransferChainOwnershipTx(t)
	case *RegisterL1ValidatorTx:
		return marshalRegisterL1ValidatorTx(t)
	case *ImportTx:
		return marshalImportTx(t)
	case *ExportTx:
		return marshalExportTx(t)
	case *CreateChainTx:
		return marshalCreateChainTx(t)
	default:
		return nil, fmt.Errorf("zap_native: unsigned tx type %T not yet bridged", u)
	}
}

// marshalBaseTx encodes a standalone txs.BaseTx (a full spending tx) as a
// native-ZAP TxKindBaseFull object: the shared spending envelope with no
// delta fields.
func marshalBaseTx(tx *BaseTx) ([]byte, error) {
	outs, err := toOutputEntries(tx.Outs)
	if err != nil {
		return nil, err
	}
	ins, err := toInputEntries(tx.Ins)
	if err != nil {
		return nil, err
	}
	capHint := zap.HeaderSize + 32 + SpendEnvelopeSize +
		len(outs)*zn.SizeTransferableOutputFull +
		len(ins)*zn.SizeTransferableInputFull + len(tx.Memo)
	b := zap.NewBuilder(capHint)
	so := writeSpendingLists(b, outs, ins)
	ob := b.StartObject(SpendEnvelopeSize)
	setSpendingEnvelope(ob, zn.TxKindBaseFull, tx.NetworkID, tx.BlockchainID, so, tx.Memo)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

// unmarshalUnsignedNative decodes the leading self-delimiting ZAP message in
// b into a concrete UnsignedTx, returning the tx and the number of bytes it
// consumed (its ZAP buffer length). Dispatch is on the TxKind discriminator
// byte at object offset 0 — no version, no slot map.
func unmarshalUnsignedNative(b []byte) (UnsignedTx, int, error) {
	n, err := zapBufferLen(b)
	if err != nil {
		return nil, 0, err
	}
	buf := b[:n]

	kind, err := txKindOf(buf)
	if err != nil {
		return nil, 0, err
	}

	switch kind {
	case zn.TxKindAdvanceTime:
		w, err := zn.WrapAdvanceTimeTx(buf)
		if err != nil {
			return nil, 0, err
		}
		tx := &AdvanceTimeTx{Time: w.Time()}
		tx.SetBytes(buf)
		return tx, n, nil

	case zn.TxKindRewardValidator:
		w, err := zn.WrapRewardValidatorTx(buf)
		if err != nil {
			return nil, 0, err
		}
		tx := &RewardValidatorTx{TxID: w.TxID()}
		tx.SetBytes(buf)
		return tx, n, nil

	case zn.TxKindBaseFull:
		msg, err := zap.Parse(buf)
		if err != nil {
			return nil, 0, err
		}
		tx := &BaseTx{BaseTx: readSpending(msg.Root())}
		tx.SetBytes(buf)
		return tx, n, nil

	case zn.TxKindIncreaseL1ValidatorBalance:
		tx, err := unmarshalIncreaseL1ValidatorBalanceTx(buf)
		return tx, n, err
	case zn.TxKindSetL1ValidatorWeight:
		tx, err := unmarshalSetL1ValidatorWeightTx(buf)
		return tx, n, err
	case zn.TxKindDisableL1Validator:
		tx, err := unmarshalDisableL1ValidatorTx(buf)
		return tx, n, err
	case zn.TxKindRemoveChainValidator:
		tx, err := unmarshalRemoveChainValidatorTx(buf)
		return tx, n, err
	case zn.TxKindCreateNetwork:
		tx, err := unmarshalCreateNetworkTx(buf)
		return tx, n, err
	case zn.TxKindTransferChainOwnership:
		tx, err := unmarshalTransferChainOwnershipTx(buf)
		return tx, n, err
	case zn.TxKindRegisterL1Validator:
		tx, err := unmarshalRegisterL1ValidatorTx(buf)
		return tx, n, err
	case zn.TxKindImport:
		tx, err := unmarshalImportTx(buf)
		return tx, n, err
	case zn.TxKindExport:
		tx, err := unmarshalExportTx(buf)
		return tx, n, err
	case zn.TxKindCreateChain:
		tx, err := unmarshalCreateChainTx(buf)
		return tx, n, err

	default:
		return nil, 0, fmt.Errorf("zap_native: tx kind %d not yet bridged", kind)
	}
}

// txKindOf reads the TxKind discriminator from a native-ZAP tx buffer. It
// parses the ZAP header (magic/version/size checked) and returns the byte at
// object offset 0. Wrap*Tx re-checks the kind against the expected value;
// this is the pre-dispatch read.
func txKindOf(buf []byte) (zn.TxKind, error) {
	msg, err := zap.Parse(buf)
	if err != nil {
		return 0, err
	}
	return zn.TxKind(msg.Root().Uint8(zn.OffsetTxKind)), nil
}

// credsBuffer object layout: CredsList @ 0, SigArray @ 8 (each an 8-byte list
// pointer). The signatures live in the shared SignatureArray that the
// CredentialList entries slice into.
const (
	offsetCredsBuf_CredsList = 0
	offsetCredsBuf_SigArray  = 8
	sizeCredsBuf             = 16
)

// marshalCredsNative encodes a signed tx's credential list as a standalone
// native-ZAP buffer appended after the unsigned prefix (unsigned ‖ creds).
// Only reached when Creds is non-empty; proposal txs never hit this path.
func marshalCredsNative(creds []verify.Verifiable) ([]byte, error) {
	credEntries, err := toCredEntries(creds)
	if err != nil {
		return nil, err
	}
	capHint := zap.HeaderSize + 32 + sizeCredsBuf + len(creds)*zn.SizeCredential
	b := zap.NewBuilder(capHint)
	credsOff, credsCount, sigBlobs := zn.WriteCredentialList(b, credEntries)
	sigArrOff, sigArrCount := zn.WriteSignatureArray(b, sigBlobs)
	ob := b.StartObject(sizeCredsBuf)
	ob.SetList(offsetCredsBuf_CredsList, credsOff, credsCount)
	ob.SetList(offsetCredsBuf_SigArray, sigArrOff, sigArrCount)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

// unmarshalCredsNative decodes a credential buffer produced by
// marshalCredsNative back into the []verify.Verifiable credential slice.
func unmarshalCredsNative(b []byte) ([]verify.Verifiable, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	credsList := zn.CredentialListView(obj, offsetCredsBuf_CredsList)
	sigArr := zn.SignatureArrayView(obj, offsetCredsBuf_SigArray)
	return fromCredEntries(credsList, sigArr), nil
}
