// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/node/quasar"
)

// adaptToQuasarContext converts a quasar.Context to a quasar.Context
// This is a temporary adapter until the contexts are unified
func adaptToQuasarContext(ctx *quasar.Context) *quasar.Context {
	return &quasar.Context{
		NetworkID:  ctx.NetworkID,
		SubnetID:   ctx.SubnetID,
		ChainID:    ctx.ChainID,
		NodeID:     ctx.NodeID,
		LUXAssetID: ctx.LUXAssetID,
		// Other fields will use default values
	}
}