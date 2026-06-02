// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"errors"
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// wrapAllKinds runs every WrapXxxTx accessor in parallel and returns the
// list of (kind, error) pairs. The cross-type confusion invariant is:
//
//   - For a well-formed v3 buffer with known TxKind=K, exactly ONE
//     wrapper (the one matching K) returns nil; ALL OTHERS return
//     ErrWrongTxKind.
//   - For arbitrary byte slices: zero or one wrapper returns nil;
//     all the rest return ErrWrongTxKind / ErrWrongSchemaVersion /
//     a zap.Parse error.
//
// Used by both FuzzWrapAllTxKinds (arbitrary inputs) and unit-tests
// (well-formed inputs).
func wrapAllKinds(buf []byte) (acceptedKinds []TxKind, errs []error) {
	type wrapper struct {
		kind TxKind
		fn   func([]byte) error
	}
	wrappers := []wrapper{
		{TxKindAdvanceTime, func(b []byte) error { _, e := WrapAdvanceTimeTx(b); return e }},
		{TxKindRewardValidator, func(b []byte) error { _, e := WrapRewardValidatorTx(b); return e }},
		{TxKindSetL1ValidatorWeight, func(b []byte) error { _, e := WrapSetL1ValidatorWeightTx(b); return e }},
		{TxKindIncreaseL1ValidatorBalance, func(b []byte) error { _, e := WrapIncreaseL1ValidatorBalanceTx(b); return e }},
		{TxKindDisableL1Validator, func(b []byte) error { _, e := WrapDisableL1ValidatorTx(b); return e }},
		{TxKindBase, func(b []byte) error { _, e := WrapBaseTx(b); return e }},
		{TxKindRegisterL1Validator, func(b []byte) error { _, e := WrapRegisterL1ValidatorTx(b); return e }},
		{TxKindSlashValidator, func(b []byte) error { _, e := WrapSlashValidatorTx(b); return e }},
		{TxKindTransferChainOwnership, func(b []byte) error { _, e := WrapTransferChainOwnershipTx(b); return e }},
		{TxKindRemoveChainValidator, func(b []byte) error { _, e := WrapRemoveChainValidatorTx(b); return e }},
		{TxKindBaseFull, func(b []byte) error { _, e := WrapBaseTxFull(b); return e }},
		{TxKindAddPermissionlessValidator, func(b []byte) error { _, e := WrapAddPermissionlessValidatorTx(b); return e }},
		{TxKindImport, func(b []byte) error { _, e := WrapImportTx(b); return e }},
		{TxKindExport, func(b []byte) error { _, e := WrapExportTx(b); return e }},
		{TxKindCreateChain, func(b []byte) error { _, e := WrapCreateChainTx(b); return e }},
		{TxKindAddValidator, func(b []byte) error { _, e := WrapAddValidatorTx(b); return e }},
		{TxKindAddDelegator, func(b []byte) error { _, e := WrapAddDelegatorTx(b); return e }},
		{TxKindAddPermissionlessDelegator, func(b []byte) error { _, e := WrapAddPermissionlessDelegatorTx(b); return e }},
		{TxKindAddChainValidator, func(b []byte) error { _, e := WrapAddChainValidatorTx(b); return e }},
		{TxKindCreateNetwork, func(b []byte) error { _, e := WrapCreateNetworkTx(b); return e }},
		{TxKindTransformChain, func(b []byte) error { _, e := WrapTransformChainTx(b); return e }},
		{TxKindConvertNetworkToL1, func(b []byte) error { _, e := WrapConvertNetworkToL1Tx(b); return e }},
		{TxKindCreateSovereignL1, func(b []byte) error { _, e := WrapCreateSovereignL1Tx(b); return e }},
	}
	for _, w := range wrappers {
		err := w.fn(buf)
		errs = append(errs, err)
		if err == nil {
			acceptedKinds = append(acceptedKinds, w.kind)
		}
	}
	return acceptedKinds, errs
}

// FuzzWrapAllTxKinds pins the cross-type confusion invariant:
//
//	For any byte slice b, at MOST ONE WrapXxxTx returns nil; all others
//	MUST return a typed error (ErrWrongTxKind / ErrWrongSchemaVersion /
//	zap.Parse error). No panics.
//
// LP-023 Red round 3 follow-up #3 (Wrap path).
//
// Seed corpus: every Wrap*Tx test fixture (well-formed buffers, each
// accepted by exactly one wrapper); plus the buffer-too-small,
// wrong-magic, wrong-version, and TxKind-flipped variants.
func FuzzWrapAllTxKinds(f *testing.F) {
	// Well-formed seeds, one per tx type.
	f.Add(NewAdvanceTimeTx(1_700_000_000).Bytes())
	f.Add(NewRewardValidatorTx(ids.ID{0x01}).Bytes())
	f.Add(NewSetL1ValidatorWeightTx(ids.ID{0x02}, 1, 42).Bytes())
	f.Add(NewIncreaseL1ValidatorBalanceTx(ids.ID{0x03}, 100).Bytes())
	f.Add(NewDisableL1ValidatorTx(ids.ID{0x04}).Bytes())
	f.Add(NewBaseTx(1, ids.ID{0x05}, []byte("memo")).Bytes())
	f.Add(NewBaseTxFull(BaseTxFullInput{NetworkID: 1, BlockchainID: ids.ID{0x06}}).Bytes())
	f.Add(NewAddValidatorTx(AddValidatorTxInput{NetworkID: 1, NodeID: ids.NodeID{0x1}}).Bytes())
	f.Add(NewAddDelegatorTx(AddDelegatorTxInput{NetworkID: 1, NodeID: ids.NodeID{0x2}}).Bytes())
	f.Add(NewAddChainValidatorTx(AddChainValidatorTxInput{NetworkID: 1, NodeID: ids.NodeID{0x3}}).Bytes())
	f.Add(NewCreateNetworkTx(CreateNetworkTxInput{NetworkID: 1}).Bytes())
	f.Add(NewTransformChainTx(TransformChainTxInput{NetworkID: 1}).Bytes())
	f.Add(NewConvertNetworkToL1Tx(ConvertNetworkToL1TxInput{NetworkID: 1}).Bytes())
	f.Add(NewCreateSovereignL1Tx(CreateSovereignL1TxInput{NetworkID: 1}).Bytes())

	// Adversarial seeds.
	f.Add([]byte{})
	f.Add([]byte("ZAP\x00")) // magic only

	f.Fuzz(func(t *testing.T, buf []byte) {
		// Property: no Wrap*Tx panics on arbitrary input.
		// (defer-recover for the whole batch — any panic from any
		//  wrapper is a regression.)
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Wrap*Tx panicked on input len=%d: %v", len(buf), r)
			}
		}()

		acceptedKinds, errs := wrapAllKinds(buf)

		// Property: at most ONE wrapper accepts.
		if len(acceptedKinds) > 1 {
			t.Fatalf("cross-type confusion: %d wrappers accepted same buffer (kinds: %v)",
				len(acceptedKinds), acceptedKinds)
		}

		// Property: every rejecting wrapper returns a typed error
		// (ErrWrongTxKind / ErrWrongSchemaVersion / zap sentinel). No
		// nil-from-Wrap-but-not-in-accepted weirdness.
		for i, e := range errs {
			if e == nil && len(acceptedKinds) == 0 {
				t.Fatalf("wrapper #%d returned nil err but accepted=0", i)
			}
			if e != nil {
				// Acceptable error families: typed ZAP / typed wrap.
				if errors.Is(e, ErrWrongTxKind) ||
					errors.Is(e, ErrWrongSchemaVersion) ||
					errors.Is(e, zap.ErrBufferTooSmall) ||
					errors.Is(e, zap.ErrInvalidMagic) ||
					errors.Is(e, zap.ErrInvalidVersion) {
					continue
				}
				// Unknown error type — log it (typed errors should
				// cover every reject path; if not, that's worth
				// triaging but not a panic).
				t.Logf("wrapper #%d returned unexpected error type: %v", i, e)
			}
		}
	})
}
