// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"github.com/luxfi/consensus/runtime"
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/platformvm/txs/fee"
)

const Alias = "P"

type Context struct {
	NetworkID         uint32
	ChainID           ids.ID // Optional: if set, overrides default PlatformChainID
	XAssetID          ids.ID
	ComplexityWeights gas.Dimensions
	GasPrice          gas.Price
	StaticFeeConfig   fee.StaticConfig
}

func NewConsensusContext(networkID uint32, luxAssetID ids.ID) (*runtime.Runtime, error) {
	return NewConsensusContextWithChainID(networkID, constants.PlatformChainID, luxAssetID)
}

func NewConsensusContextWithChainID(networkID uint32, chainID ids.ID, luxAssetID ids.ID) (*runtime.Runtime, error) {
	lookup := ids.NewAliaser()
	ctx := &runtime.Runtime{
		NetworkID: networkID,
		ChainID:   chainID,
		XAssetID:  luxAssetID,
	}
	return ctx, lookup.Alias(chainID, Alias)
}
