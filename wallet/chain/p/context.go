// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"context"

	"github.com/luxfi/node/v2/api/info"
	"github.com/luxfi/node/v2/utils/constants"
	"github.com/luxfi/node/v2/vms/platformvm"
	"github.com/luxfi/node/v2/wallet/chain/p/builder"
)

// gasPriceMultiplier increases the gas price to support multiple transactions
// to be issued.
//
// TODO: Handle this better. Either here or in the mempool.
const gasPriceMultiplier = 2

func NewContextFromURI(ctx context.Context, uri string) (*builder.Context, error) {
	infoClient := info.NewClient(uri)
	chainClient := platformvm.NewClient(uri)
	return NewContextFromClients(ctx, infoClient, chainClient)
}

func NewContextFromClients(
	ctx context.Context,
	infoClient *info.Client,
	chainClient *platformvm.Client,
) (*builder.Context, error) {
	networkID, err := infoClient.GetNetworkID(ctx)
	if err != nil {
		return nil, err
	}

	luxAssetID, err := chainClient.GetStakingAssetID(ctx, constants.PrimaryNetworkID)
	if err != nil {
		return nil, err
	}

	dynamicFeeConfig, err := chainClient.GetFeeConfig(ctx)
	if err != nil {
		return nil, err
	}

	_, gasPrice, _, err := chainClient.GetFeeState(ctx)
	if err != nil {
		return nil, err
	}

	return &builder.Context{
		NetworkID:         networkID,
		LUXAssetID:        luxAssetID,
		ComplexityWeights: dynamicFeeConfig.Weights,
		GasPrice:          gasPriceMultiplier * gasPrice,
	}, nil
}
