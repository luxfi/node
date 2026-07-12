// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/runtime"
	"github.com/luxfi/utils"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var (
	_ UnsignedTx = (*ImportTx)(nil)

	errNoImportInputs = errors.New("tx has no imported inputs")
)

// ImportTx consumes funds produced on another chain. The struct IS the wire:
// it embeds the spending envelope and reads its delta fields by offset.
//
// Delta layout (fixed section, after the 77-byte spending envelope):
//
//	SourceChain    32B @ 77   (id: chain to consume the funds from)
//	ImportedInputs 8B  @ 109  (input list ptr)
//	SigIndices     8B  @ 117  (shared input sig-index array ptr)
type ImportTx struct {
	spendingTx
}

const (
	offImportSourceChain = spendSize // 77
	offImportInputs      = 109
	offImportSigIndices  = 117
	sizeImport           = 125
)

// NewImportTx builds the tx into a fresh zap buffer.
func NewImportTx(base *lux.BaseTx, sourceChain ids.ID, importedInputs []*lux.TransferableInput) (*ImportTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 512 + sizeImport)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	listOff, listCount, sigOff, sigCount, err := writeExtraIns(b, importedInputs)
	if err != nil {
		return nil, err
	}

	ob := b.StartObject(sizeImport)
	setEnvelope(ob, kindImport, base, p)
	setID(ob, offImportSourceChain, sourceChain)
	ob.SetList(offImportInputs, listOff, listCount)
	ob.SetList(offImportSigIndices, sigOff, sigCount)
	ob.FinishAsRoot()

	msg, _ := zap.Parse(b.Finish())
	return &ImportTx{spendingTx{msg}}, nil
}

// SourceChain is the chain the imported funds are consumed from (offset read).
func (tx *ImportTx) SourceChain() ids.ID { return readID(tx.root(), offImportSourceChain) }

// ImportedInputs are the inputs consuming UTXOs produced on the source chain.
func (tx *ImportTx) ImportedInputs() []*lux.TransferableInput {
	return readExtraIns(tx.root(), offImportInputs, offImportSigIndices)
}

// InputUTXOs returns the UTXOIDs of the imported funds.
func (tx *ImportTx) InputUTXOs() set.Set[ids.ID] {
	ins := tx.ImportedInputs()
	s := set.NewSet[ids.ID](len(ins))
	for _, in := range ins {
		s.Add(in.InputID())
	}
	return s
}

func (tx *ImportTx) InputIDs() set.Set[ids.ID] {
	inputs := tx.spendingTx.InputIDs()
	return inputs.Union(tx.InputUTXOs())
}

// SyntacticVerify this transaction is well-formed.
func (tx *ImportTx) SyntacticVerify(rt *runtime.Runtime) error {
	if tx == nil {
		return ErrNilTx
	}
	ins := tx.ImportedInputs()
	if len(ins) == 0 {
		return errNoImportInputs
	}

	if err := verifyBaseTx(tx.baseTx(), rt); err != nil {
		return err
	}

	for _, in := range ins {
		if err := in.Verify(); err != nil {
			return fmt.Errorf("input failed verification: %w", err)
		}
	}
	if !utils.IsSortedAndUnique(ins) {
		return errInputsNotSortedUnique
	}
	return nil
}

func (tx *ImportTx) Visit(visitor Visitor) error {
	return visitor.ImportTx(tx)
}

// Initialize is a no-op; the struct is already the wire.
func (tx *ImportTx) Initialize(ctx context.Context) error { return nil }
