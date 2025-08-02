// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar"
	"github.com/luxfi/node/utils/constants"
	log "github.com/luxfi/log"
	"github.com/luxfi/node/vms/components/gas"
)

const Alias = "P"

type Context struct {
	NetworkID         uint32
	LUXAssetID        ids.ID
	ComplexityWeights gas.Dimensions
	GasPrice          gas.Price
}

func NewLinearContext(networkID uint32, luxAssetID ids.ID) (*quasar.Context, error) {
	lookup := ids.NewAliaser()
	return &quasar.Context{
		NetworkID:  networkID,
		SubnetID:   constants.PrimaryNetworkID,
		ChainID:    constants.PlatformChainID(),
		LUXAssetID: luxAssetID,
		Log:        log.NewNoOpLogger(),
		BCLookup:   lookup,
	}, lookup.Alias(constants.PlatformChainID(), Alias)
}
