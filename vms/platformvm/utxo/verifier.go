// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import (
	"github.com/luxfi/consensus"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/vms/platformvm/fx"
)

// UTXOVerifier verifies that UTXOs are valid
type UTXOVerifier struct {
	ctx *consensus.Context
	clk *mockable.Clock
	fx  fx.Fx
}

// NewVerifier creates a new UTXO verifier
func NewVerifier(ctx *consensus.Context, clk *mockable.Clock, fx fx.Fx) *UTXOVerifier {
	return &UTXOVerifier{
		ctx: ctx,
		clk: clk,
		fx:  fx,
	}
}