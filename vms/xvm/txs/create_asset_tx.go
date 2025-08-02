// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/node/v2/quasar"
	"github.com/luxfi/node/v2/vms/secp256k1fx"
)

var (
	_ UnsignedTx             = (*CreateAssetTx)(nil)
	_ secp256k1fx.UnsignedTx = (*CreateAssetTx)(nil)
)

// CreateAssetTx is a transaction that creates a new asset.
type CreateAssetTx struct {
	BaseTx       `serialize:"true"`
	Name         string          `serialize:"true" json:"name"`
	Symbol       string          `serialize:"true" json:"symbol"`
	Denomination byte            `serialize:"true" json:"denomination"`
	States       []*InitialState `serialize:"true" json:"initialStates"`
}

func (t *CreateAssetTx) InitCtx(ctx *quasar.Context) {
	for _, state := range t.States {
		state.InitCtx(ctx)
	}
	t.BaseTx.InitCtx(ctx)
}

// Initialize implements quasar.ContextInitializable
func (t *CreateAssetTx) Initialize(ctx *quasar.Context) error {
	t.InitCtx(ctx)
	return nil
}

// InitialStates track which virtual machines, and the initial state of these
// machines, this asset uses. The returned array should not be modified.
func (t *CreateAssetTx) InitialStates() []*InitialState {
	return t.States
}

func (t *CreateAssetTx) Visit(v Visitor) error {
	return v.CreateAssetTx(t)
}
