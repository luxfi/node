// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/quasar"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/components/verify"
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

func (t *BaseTx) InitCtx(ctx *quasar.Context) {
	for _, out := range t.Outs {
		out.InitCtx(ctx)
	}
}

// Initialize implements quasar.ContextInitializable
func (t *BaseTx) Initialize(ctx *quasar.Context) error {
	t.InitCtx(ctx)
	return nil
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

func (t *BaseTx) Visit(v Visitor) error {
	return v.BaseTx(t)
}

// NumCredentials returns the number of expected credentials
func (t *BaseTx) NumCredentials() int {
	return t.BaseTx.NumCredentials()
}

// SyntacticVerify returns nil iff this tx is well formed
func (t *BaseTx) SyntacticVerify(
	ctx *quasar.Context,
	c codec.Manager,
	txFeeAssetID ids.ID,
	txFee uint64,
	createSubnetTxFee uint64,
	numIns int,
) error {
	// Verify the embedded lux.BaseTx
	if err := t.BaseTx.Verify(ctx); err != nil {
		return err
	}

	// Additional verification can be added here
	return nil
}

// SemanticVerify returns nil iff this tx is valid
func (t *BaseTx) SemanticVerify(vm VM, tx UnsignedTx, creds []verify.Verifiable) error {
	// Semantic verification logic would go here
	// For now, just return nil
	return nil
}
