// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"github.com/luxfi/node/v2/quasar"
	"github.com/luxfi/node/v2/vms/components/gas"
	"github.com/luxfi/node/v2/vms/platformvm/config"
	"github.com/luxfi/node/v2/vms/platformvm/state"
	"github.com/luxfi/node/v2/wallet/chain/p/builder"
)

func newContext(
	ctx *quasar.Context,
	config *config.Internal,
	state state.State,
) *builder.Context {
	builderContext := &builder.Context{
		NetworkID:  ctx.NetworkID,
		LUXAssetID: ctx.LUXAssetID,
	}

	builderContext.ComplexityWeights = config.DynamicFeeConfig.Weights
	builderContext.GasPrice = gas.CalculatePrice(
		config.DynamicFeeConfig.MinPrice,
		state.GetFeeState().Excess,
		config.DynamicFeeConfig.ExcessConversionConstant,
	)

	return builderContext
}
