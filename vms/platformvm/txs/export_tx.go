// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var (
	_ UnsignedTx = (*ExportTx)(nil)

	ErrWrongLocktime   = errors.New("wrong locktime reported")
	errNoExportOutputs = errors.New("no export outputs")
)

// ExportTx sends funds to another chain. The struct IS the wire: it embeds the
// spending envelope and reads its delta fields by offset.
//
// Delta layout (fixed section, after the 77-byte spending envelope):
//
//	DestinationChain 32B @ 77   (id: chain to send the funds to)
//	ExportedOutputs  8B  @ 109  (output list ptr)
//	OwnerAddrs       8B  @ 117  (shared owner-address array ptr)
type ExportTx struct {
	spendingTx
}

const (
	offExportDestChain = spendSize // 77
	offExportOutputs   = 109
	offExportAddrs     = 117
	sizeExport         = 125
)

// NewExportTx builds the tx into a fresh zap buffer.
func NewExportTx(base *lux.BaseTx, destinationChain ids.ID, exportedOutputs []*lux.TransferableOutput) (*ExportTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 512 + sizeExport)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	listOff, listCount, addrOff, addrCount, err := writeExtraOuts(b, exportedOutputs)
	if err != nil {
		return nil, err
	}

	ob := b.StartObject(sizeExport)
	setEnvelope(ob, kindExport, base, p)
	setID(ob, offExportDestChain, destinationChain)
	ob.SetList(offExportOutputs, listOff, listCount)
	ob.SetList(offExportAddrs, addrOff, addrCount)
	ob.FinishAsRoot()

	msg, _ := zap.Parse(b.Finish())
	return &ExportTx{spendingTx{msg}}, nil
}

// DestinationChain is the chain the exported funds are sent to (offset read).
func (tx *ExportTx) DestinationChain() ids.ID { return readID(tx.root(), offExportDestChain) }

// ExportedOutputs are the outputs exported to the destination chain.
func (tx *ExportTx) ExportedOutputs() []*lux.TransferableOutput {
	return readExtraOuts(tx.root(), offExportOutputs, offExportAddrs)
}

// SyntacticVerify this transaction is well-formed.
func (tx *ExportTx) SyntacticVerify(rt *runtime.Runtime) error {
	if tx == nil {
		return ErrNilTx
	}
	outs := tx.ExportedOutputs()
	if len(outs) == 0 {
		return errNoExportOutputs
	}

	if err := verifyBaseTx(tx.baseTx(), rt); err != nil {
		return err
	}

	for _, out := range outs {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("output failed verification: %w", err)
		}
		if _, ok := out.Output().(*stakeable.LockOut); ok {
			return ErrWrongLocktime
		}
	}
	if !lux.IsSortedTransferableOutputs(outs) {
		return errOutputsNotSorted
	}
	return nil
}

func (tx *ExportTx) Visit(visitor Visitor) error {
	return visitor.ExportTx(tx)
}

// Initialize is a no-op; the struct is already the wire.
func (tx *ExportTx) Initialize(ctx context.Context) error { return nil }
