// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	consensusctx "github.com/luxfi/consensus/context"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/components/gas"
)

const Alias = "P"

type Context struct {
	NetworkID         uint32
	LUXAssetID       ids.ID
	ComplexityWeights gas.Dimensions
	GasPrice          gas.Price
}

func NewConsensusContext(networkID uint32, luxAssetID ids.ID) (*consensusctx.Context, error) {
	lookup := ids.NewAliaser()
	ctx := &consensusctx.Context{
		NetworkID:   networkID,
		NetID:    constants.PrimaryNetworkID,
		ChainID:     constants.PlatformChainID,
		LUXAssetID: luxAssetID,
	}
	return ctx, lookup.Alias(constants.PlatformChainID, Alias)
}
