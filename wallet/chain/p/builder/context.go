// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/vms/components/gas"
)

const Alias = "P"

type Context struct {
	NetworkID         uint32
	LUXAssetID       ids.ID
	ComplexityWeights gas.Dimensions
	GasPrice          gas.Price
}

func NewSnowContext(networkID uint32, luxAssetID ids.ID) (*consensus.Context, error) {
	lookup := ids.NewAliaser()
	return &consensus.Context{
		NetworkID:   networkID,
		SubnetID:    constants.PrimaryNetworkID,
		ChainID:     constants.PlatformChainID,
		LUXAssetID: luxAssetID,
		Log:         logging.NoLog{},
		BCLookup:    lookup,
	}, lookup.Alias(constants.PlatformChainID, Alias)
}
