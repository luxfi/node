// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/secp256k1fx"
)

var (
	_ UnsignedTx             = (*BaseTx)(nil)
	_ secp256k1fx.UnsignedTx = (*BaseTx)(nil)
)

// BaseTx is the basis of all transactions.
type BaseTx struct {
	lux.BaseTx `serialize:"true"`

	bytes []byte
}

func (t *BaseTx) InitCtx(ctx context.Context) {
	for _, out := range t.Outs {
		out.InitCtx(ctx)
	}
}

func (t *BaseTx) SetBytes(bytes []byte) {
	t.bytes = bytes
}

func (t *BaseTx) Bytes() []byte {
	return t.bytes
}

func (t *BaseTx) InputIDs() set.Set[ids.ID] {
	inputIDs := set.NewSet[ids.ID](len(t.Ins))
	for _, in := range t.Ins {
		inputIDs.Add(in.InputID())
	}
	return inputIDs
}

// InputUTXOs returns the UTXOIDs this transaction is consuming
func (t *BaseTx) InputUTXOs() []*lux.UTXOID {
	utxos := make([]*lux.UTXOID, len(t.Ins))
	for i, in := range t.Ins {
		utxos[i] = &in.UTXOID
	}
	return utxos
}

func (t *BaseTx) Visit(v Visitor) error {
	return v.BaseTx(t)
}

// NumCredentials returns the number of expected credentials
func (t *BaseTx) NumCredentials() int {
	return len(t.Ins)
}

// InitializeWithContext initializes the transaction with consensus context
func (tx *BaseTx) InitializeWithContext(ctx context.Context) error {
    // Initialize any context-dependent fields here
    return nil
}
