// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/wallet/chain/p/builder"
)

func newContext(
	ctx *consensus.Context,
	config *config.Internal,
	state state.State,
) *builder.Context {
	builderContext := &builder.Context{
		NetworkID:   ctx.NetworkID,
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
