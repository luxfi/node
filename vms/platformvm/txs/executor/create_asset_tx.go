// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"

	"github.com/luxfi/node/vms/platformvm/status"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// CreateAssetTx mints a new UTXO asset on the P-Chain.
//
// The asset ID is, by convention with the X-Chain wire format, the tx ID.
// The fee charged is computed by the fee calculator (static config →
// `CreateAssetTxFee`; dynamic fee → gas complexity); the minted asset is
// produced as UTXOs owned by [tx.States[*].Outs] keyed by the new asset ID.
func (e *standardTxExecutor) CreateAssetTx(tx *txs.CreateAssetTx) error {
	// Verify the tx is well-formed first.
	if err := e.tx.SyntacticVerify(e.backend.Runtime); err != nil {
		return err
	}

	// Fee flow check.
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Ins,
		tx.Outs,
		e.tx.Creds,
		map[ids.ID]uint64{
			e.backend.Runtime.UTXOAssetID: fee,
		},
	); err != nil {
		return err
	}

	txID := e.tx.ID()

	// Consume the fee inputs, produce the fee change outputs (BaseTx outs).
	lux.Consume(e.state, tx.Ins)
	lux.Produce(e.state, txID, tx.Outs)

	// Produce the freshly minted asset UTXOs. The asset ID is the tx ID.
	//
	// Output indices come from the InitialState slot position, NOT from the
	// count of UTXOs we actually wrote. Mirrors XVM semantics: a mint output
	// at States[0].Outs[0] gets `OutputIndex = len(tx.Outs)` even though the
	// executor doesn't write the mint UTXO here (mint authority is held in
	// the asset registry, materialised on first OperationTx mint). A holder
	// transfer output at States[0].Outs[1] gets `OutputIndex = len(tx.Outs)+1`.
	// Compacting indices would break OperationTx references back to mint
	// outputs (RED V4 outputIndex-phantom-UTXO vector).
	outputIndex := uint32(len(tx.Outs))
	for _, state := range tx.States {
		for _, out := range state.Outs {
			if transferOut, ok := out.(lux.TransferableOut); ok {
				utxo := &lux.UTXO{
					UTXOID: lux.UTXOID{
						TxID:        txID,
						OutputIndex: outputIndex,
					},
					Asset: lux.Asset{ID: txID},
					Out:   transferOut,
				}
				e.state.AddUTXO(utxo)
			}
			// Non-TransferableOut (e.g. MintOutput) — index slot reserved
			// for OperationTx references; UTXO materialised on first mint.
			outputIndex++
		}
	}

	// Persist the create-asset tx itself so future ops, SemanticVerifier-style
	// lookups (`State.GetTx(assetID)`) and indexer projections
	// (`chain.PrimaryAlias(assetID)`) resolve to the original tx with all
	// metadata. The asset ID is the tx ID by construction (line 43 above);
	// `state.AddTx` is the canonical PlatformVM persistence API used by
	// CreateChainTx / CreateNetworkTx / committed-block paths.
	e.state.AddTx(e.tx, status.Committed)
	return nil
}
