// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"

	"github.com/luxfi/node/vms/platformvm/status"
	"github.com/luxfi/node/vms/platformvm/txs"
)

var (
	errOpUTXOMissingAssetID = errors.New("op consumes a UTXO whose asset ID does not match the op asset")
)

// OperationTx applies the operations carried by [tx] to the P-Chain UTXO set.
//
// On the P-only primary network we accept a single Fx (secp256k1fx). Each
// Operation consumes a UTXO set whose asset ID matches [op.Asset.ID]. The
// emitted side effects are encoded by the Fx-specific Op, and every Op is
// authorised by an explicit credential — mirroring `xvm/txs/executor/
// semantic_verifier.go::verifyOperation`. A missing or invalid credential
// rejects the entire tx before any state mutation.
func (e *standardTxExecutor) OperationTx(tx *txs.OperationTx) error {
	if err := e.tx.SyntacticVerify(e.backend.Runtime); err != nil {
		return err
	}

	// Defensive cred-count guard. The Tx wire format is fixed by the codec
	// but a malicious peer could ship a payload that decoded with fewer
	// creds than [NumCredentials]. Without this check the BaseTx cred
	// slice below — and the per-Op cred lookup further down — would slice
	// past the end and panic. Mirrors xvm/txs/executor/syntactic_verifier
	// .go::OperationTx.
	expectedCreds := tx.NumCredentials()
	if len(e.tx.Creds) != expectedCreds {
		return fmt.Errorf("%w: %d != %d",
			txs.ErrWrongNumberOfCredentials,
			len(e.tx.Creds),
			expectedCreds,
		)
	}

	// Charge the standard TxFee out of the BaseTx inputs / outputs.
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Ins,
		tx.Outs,
		e.tx.Creds[:len(tx.Ins)],
		map[ids.ID]uint64{
			e.backend.Runtime.XAssetID: fee,
		},
	); err != nil {
		return fmt.Errorf("op tx fee verification: %w", err)
	}

	// Authorise + collect each operation against the chain state. We loop
	// twice on purpose:
	//
	//   1. Resolve every consumed UTXO and call fx.VerifyOperation so that
	//      ANY auth failure aborts the tx before we mutate state.
	//   2. Once every op has been authorised, delete the consumed UTXOs.
	//
	// The two-pass shape matches xvm's verify-then-apply discipline: any
	// auth failure leaves the UTXO set untouched.
	opCredOffset := len(tx.Ins)
	consumedUTXOIDs := make([]ids.ID, 0, len(tx.Ops))
	for i, op := range tx.Ops {
		if err := e.verifyOperation(tx, op, e.tx.Creds[opCredOffset+i]); err != nil {
			return fmt.Errorf("op %d verification: %w", i, err)
		}
		for _, utxoID := range op.UTXOIDs {
			consumedUTXOIDs = append(consumedUTXOIDs, utxoID.InputID())
		}
	}
	for _, id := range consumedUTXOIDs {
		e.state.DeleteUTXO(id)
	}

	txID := e.tx.ID()

	// Consume the BaseTx fee inputs / produce BaseTx outputs.
	lux.Consume(e.state, tx.Ins)
	lux.Produce(e.state, txID, tx.Outs)

	// Persist the tx itself so future operations / indexer queries resolve
	// `state.GetTx(txID)` to the committed payload. Aligns with how
	// CreateChainTx and CreateNetworkTx persist their on-chain receipt.
	e.state.AddTx(e.tx, status.Committed)
	return nil
}

// verifyOperation resolves the UTXOs consumed by [op], asserts they share
// the op's asset ID, then delegates to fx.VerifyOperation so the Fx checks
// the credential against the consumed outputs.
func (e *standardTxExecutor) verifyOperation(
	utx txs.UnsignedTx,
	op *txs.Operation,
	cred interface{},
) error {
	opAssetID := op.AssetID()
	utxos := make([]interface{}, len(op.UTXOIDs))
	for i, utxoID := range op.UTXOIDs {
		utxo, err := e.state.GetUTXO(utxoID.InputID())
		if err != nil {
			return fmt.Errorf("op references unknown utxo %s: %w", utxoID.InputID(), err)
		}
		if utxo.AssetID() != opAssetID {
			return errOpUTXOMissingAssetID
		}
		utxos[i] = utxo.Out
	}
	return e.backend.Fx.VerifyOperation(utx, op.Op, cred, utxos)
}
