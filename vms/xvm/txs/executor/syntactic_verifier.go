// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/xvm/txs"
)

const (
	minNameLen      = 1
	maxNameLen      = 128
	minSymbolLen    = 1
	maxSymbolLen    = 4
	maxDenomination = 32
)

var (
	_ txs.Visitor = (*SyntacticVerifier)(nil)

	errWrongNumberOfCredentials     = errors.New("wrong number of credentials")
	errInitialStatesNotSortedUnique = errors.New("initial states not sorted and unique")
	errNameTooShort                 = fmt.Errorf("name is too short, minimum size is %d", minNameLen)
	errNameTooLong                  = fmt.Errorf("name is too long, maximum size is %d", maxNameLen)
	errSymbolTooShort               = fmt.Errorf("symbol is too short, minimum size is %d", minSymbolLen)
	errSymbolTooLong                = fmt.Errorf("symbol is too long, maximum size is %d", maxSymbolLen)
	errNoFxs                        = errors.New("assets must support at least one Fx")
	errIllegalNameCharacter         = errors.New("asset's name must be made up of only letters and numbers")
	errIllegalSymbolCharacter       = errors.New("asset's symbol must be all upper case letters")
	errUnexpectedWhitespace         = errors.New("unexpected whitespace provided")
	errDenominationTooLarge         = errors.New("denomination is too large")
	errOperationsNotSortedUnique    = errors.New("operations not sorted and unique")
	errNoOperations                 = errors.New("an operationTx must have at least one operation")
	errDoubleSpend                  = errors.New("inputs attempt to double spend an input")
	errNoImportInputs               = errors.New("no import inputs")
	errNoExportOutputs              = errors.New("no export outputs")
)

type SyntacticVerifier struct {
	*Backend
	Tx *txs.Tx
}

// quasarContext returns a quasar.Context created from the consensus.Context
func (v *SyntacticVerifier) quasarContext() *quasar.Context {
	return &quasar.Context{
		NetworkID:  v.Ctx.NetworkID,
		ChainID:    v.Ctx.ChainID,
		SubnetID:   v.Ctx.SubnetID,
		NodeID:     v.Ctx.NodeID,
		LUXAssetID: v.Ctx.LUXAssetID,
		Log:        v.Ctx.Log,
	}
}

func (v *SyntacticVerifier) BaseTx(tx *txs.BaseTx) error {
	if err := tx.BaseTx.Verify(v.quasarContext()); err != nil {
		return err
	}

	err := lux.VerifyTx(
		v.Config.TxFee,
		v.FeeAssetID,
		[][]*lux.TransferableInput{tx.Ins},
		[][]*lux.TransferableOutput{tx.Outs},
		v.Codec,
	)
	if err != nil {
		return err
	}

	for _, cred := range v.Tx.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	numCreds := len(v.Tx.Creds)
	numInputs := len(tx.Ins)
	if numCreds != numInputs {
		return fmt.Errorf("%w: %d != %d",
			errWrongNumberOfCredentials,
			numCreds,
			numInputs,
		)
	}

	return nil
}

func (v *SyntacticVerifier) CreateAssetTx(tx *txs.CreateAssetTx) error {
	switch {
	case len(tx.Name) < minNameLen:
		return errNameTooShort
	case len(tx.Name) > maxNameLen:
		return errNameTooLong
	case len(tx.Symbol) < minSymbolLen:
		return errSymbolTooShort
	case len(tx.Symbol) > maxSymbolLen:
		return errSymbolTooLong
	case len(tx.States) == 0:
		return errNoFxs
	case tx.Denomination > maxDenomination:
		return errDenominationTooLarge
	case strings.TrimSpace(tx.Name) != tx.Name:
		return errUnexpectedWhitespace
	}

	for _, r := range tx.Name {
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsNumber(r) && r != ' ') {
			return errIllegalNameCharacter
		}
	}
	for _, r := range tx.Symbol {
		if r > unicode.MaxASCII || !unicode.IsUpper(r) {
			return errIllegalSymbolCharacter
		}
	}

	if err := tx.BaseTx.BaseTx.Verify(v.quasarContext()); err != nil {
		return err
	}

	err := lux.VerifyTx(
		v.Config.CreateAssetTxFee,
		v.FeeAssetID,
		[][]*lux.TransferableInput{tx.Ins},
		[][]*lux.TransferableOutput{tx.Outs},
		v.Codec,
	)
	if err != nil {
		return err
	}

	for _, state := range tx.States {
		if err := state.Verify(v.Codec, len(v.Fxs)); err != nil {
			return err
		}
	}
	if !utils.IsSortedAndUnique(tx.States) {
		return errInitialStatesNotSortedUnique
	}

	for _, cred := range v.Tx.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	numCreds := len(v.Tx.Creds)
	numInputs := len(tx.Ins)
	if numCreds != numInputs {
		return fmt.Errorf("%w: %d != %d",
			errWrongNumberOfCredentials,
			numCreds,
			numInputs,
		)
	}

	return nil
}

func (v *SyntacticVerifier) OperationTx(tx *txs.OperationTx) error {
	if len(tx.Ops) == 0 {
		return errNoOperations
	}

	if err := tx.BaseTx.BaseTx.Verify(v.quasarContext()); err != nil {
		return err
	}

	err := lux.VerifyTx(
		v.Config.TxFee,
		v.FeeAssetID,
		[][]*lux.TransferableInput{tx.Ins},
		[][]*lux.TransferableOutput{tx.Outs},
		v.Codec,
	)
	if err != nil {
		return err
	}

	inputs := set.NewSet[ids.ID](len(tx.Ins))
	for _, in := range tx.Ins {
		inputs.Add(in.InputID())
	}

	for _, op := range tx.Ops {
		if err := op.Verify(); err != nil {
			return err
		}
		for _, utxoID := range op.UTXOIDs {
			inputID := utxoID.InputID()
			if inputs.Contains(inputID) {
				return errDoubleSpend
			}
			inputs.Add(inputID)
		}
	}
	if !txs.IsSortedAndUniqueOperations(tx.Ops, v.Codec) {
		return errOperationsNotSortedUnique
	}

	for _, cred := range v.Tx.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	numCreds := len(v.Tx.Creds)
	numInputs := len(tx.Ins) + len(tx.Ops)
	if numCreds != numInputs {
		return fmt.Errorf("%w: %d != %d",
			errWrongNumberOfCredentials,
			numCreds,
			numInputs,
		)
	}

	return nil
}

func (v *SyntacticVerifier) ImportTx(tx *txs.ImportTx) error {
	if len(tx.ImportedIns) == 0 {
		return errNoImportInputs
	}

	if err := tx.BaseTx.BaseTx.Verify(v.quasarContext()); err != nil {
		return err
	}

	err := lux.VerifyTx(
		v.Config.TxFee,
		v.FeeAssetID,
		[][]*lux.TransferableInput{
			tx.Ins,
			tx.ImportedIns,
		},
		[][]*lux.TransferableOutput{tx.Outs},
		v.Codec,
	)
	if err != nil {
		return err
	}

	for _, cred := range v.Tx.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	numCreds := len(v.Tx.Creds)
	numInputs := len(tx.Ins) + len(tx.ImportedIns)
	if numCreds != numInputs {
		return fmt.Errorf("%w: %d != %d",
			errWrongNumberOfCredentials,
			numCreds,
			numInputs,
		)
	}

	return nil
}

func (v *SyntacticVerifier) ExportTx(tx *txs.ExportTx) error {
	if len(tx.ExportedOuts) == 0 {
		return errNoExportOutputs
	}

	if err := tx.BaseTx.BaseTx.Verify(v.quasarContext()); err != nil {
		return err
	}

	err := lux.VerifyTx(
		v.Config.TxFee,
		v.FeeAssetID,
		[][]*lux.TransferableInput{tx.Ins},
		[][]*lux.TransferableOutput{
			tx.Outs,
			tx.ExportedOuts,
		},
		v.Codec,
	)
	if err != nil {
		return err
	}

	for _, cred := range v.Tx.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	numCreds := len(v.Tx.Creds)
	numInputs := len(tx.Ins)
	if numCreds != numInputs {
		return fmt.Errorf("%w: %d != %d",
			errWrongNumberOfCredentials,
			numCreds,
			numInputs,
		)
	}

	return nil
}

func (v *SyntacticVerifier) BurnTx(tx *txs.BurnTx) error {
	// Verify the base transaction
	if err := tx.BaseTx.Verify(v.quasarContext()); err != nil {
		return err
	}

	// Verify basic burn transaction constraints
	switch {
	case tx.AssetID == ids.Empty:
		return fmt.Errorf("invalid asset ID")
	case tx.Amount == 0:
		return fmt.Errorf("invalid burn amount")
	case tx.DestChain == ids.Empty:
		return fmt.Errorf("invalid destination chain")
	case len(tx.DestAddress) == 0:
		return fmt.Errorf("invalid destination address")
	case tx.DestChain == v.Ctx.ChainID:
		return fmt.Errorf("cannot burn to same chain")
	}

	// Verify transaction fees and structure
	err := lux.VerifyTx(
		v.Config.TxFee,
		v.FeeAssetID,
		[][]*lux.TransferableInput{tx.Ins},
		[][]*lux.TransferableOutput{tx.Outs},
		v.Codec,
	)
	if err != nil {
		return err
	}

	// Ensure we're burning the correct amount
	totalIn := uint64(0)
	for _, in := range tx.Ins {
		if in.AssetID() != tx.AssetID {
			return fmt.Errorf("input asset mismatch")
		}
		totalIn += in.Input().Amount()
	}

	// Must burn exact amount (no change outputs for burn asset)
	if totalIn != tx.Amount {
		return fmt.Errorf("burn amount mismatch")
	}

	// Verify credentials
	for _, cred := range v.Tx.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	numCreds := len(v.Tx.Creds)
	numInputs := len(tx.Ins)
	if numCreds != numInputs {
		return fmt.Errorf("%w: %d != %d",
			errWrongNumberOfCredentials,
			numCreds,
			numInputs,
		)
	}

	return nil
}

func (v *SyntacticVerifier) MintTx(tx *txs.MintTx) error {
	// Verify the base transaction
	if err := tx.BaseTx.Verify(v.quasarContext()); err != nil {
		return err
	}

	// Verify basic mint transaction constraints
	switch {
	case tx.AssetID == ids.Empty:
		return fmt.Errorf("invalid asset ID")
	case tx.Amount == 0:
		return fmt.Errorf("invalid mint amount")
	case tx.SourceChain == ids.Empty:
		return fmt.Errorf("invalid source chain")
	case len(tx.BurnProof) == 0:
		return fmt.Errorf("invalid burn proof")
	case tx.SourceChain == v.Ctx.ChainID:
		return fmt.Errorf("cannot mint from same chain")
	case len(tx.MPCSignatures) < 67: // Requires 67/100 threshold
		return fmt.Errorf("insufficient MPC signatures")
	}

	// Verify transaction fees and structure
	err := lux.VerifyTx(
		v.Config.TxFee,
		v.FeeAssetID,
		[][]*lux.TransferableInput{tx.Ins},
		[][]*lux.TransferableOutput{tx.Outs},
		v.Codec,
	)
	if err != nil {
		return err
	}

	// Ensure outputs match the mint amount
	totalOut := uint64(0)
	for _, out := range tx.Outs {
		if out.AssetID() == tx.AssetID {
			totalOut += out.Output().Amount()
		}
	}

	// Must mint exact amount
	if totalOut != tx.Amount {
		return fmt.Errorf("mint amount mismatch")
	}

	// Verify credentials
	for _, cred := range v.Tx.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	// Mint transactions may have fewer credentials than inputs
	// since they're primarily authorized by MPC signatures
	numCreds := len(v.Tx.Creds)
	numInputs := len(tx.Ins)
	if numCreds > numInputs {
		return fmt.Errorf("%w: %d > %d",
			errWrongNumberOfCredentials,
			numCreds,
			numInputs,
		)
	}

	return nil
}

func (v *SyntacticVerifier) NFTTransferTx(tx *txs.NFTTransferTx) error {
	// Verify the base transaction
	if err := tx.BaseTx.Verify(v.quasarContext()); err != nil {
		return err
	}

	// Verify basic NFT transfer transaction constraints
	switch {
	case tx.DestChain == ids.Empty:
		return fmt.Errorf("invalid destination chain")
	case len(tx.Recipient) == 0:
		return fmt.Errorf("invalid recipient")
	case tx.DestChain == v.Ctx.ChainID:
		return fmt.Errorf("cannot transfer NFT to same chain")
	}

	// Verify transaction fees and structure
	err := lux.VerifyTx(
		v.Config.TxFee,
		v.FeeAssetID,
		[][]*lux.TransferableInput{tx.Ins},
		[][]*lux.TransferableOutput{tx.Outs},
		v.Codec,
	)
	if err != nil {
		return err
	}

	// Verify NFT transfer operation
	if err := tx.NFTTransferOp.Verify(); err != nil {
		return err
	}

	// Verify credentials
	for _, cred := range v.Tx.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	// NFT transfers need credentials for all inputs including the NFT
	numCreds := len(v.Tx.Creds)
	numInputs := len(tx.Ins) + 1 // +1 for the NFT input
	if numCreds != numInputs {
		return fmt.Errorf("%w: %d != %d",
			errWrongNumberOfCredentials,
			numCreds,
			numInputs,
		)
	}

	return nil
}
