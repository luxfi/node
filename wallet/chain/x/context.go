// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"context"

	"github.com/luxfi/node/v2/api/info"
	"github.com/luxfi/node/v2/vms/xvm"
	"github.com/luxfi/node/v2/wallet/chain/x/builder"
)

func NewContextFromURI(ctx context.Context, uri string) (*builder.Context, error) {
	infoClient := info.NewClient(uri)
	xChainClient := xvm.NewClient(uri, builder.Alias)
	return NewContextFromClients(ctx, infoClient, xChainClient)
}

func NewContextFromClients(
	ctx context.Context,
	infoClient *info.Client,
	xChainClient *xvm.Client,
) (*builder.Context, error) {
	networkID, err := infoClient.GetNetworkID(ctx)
	if err != nil {
		return nil, err
	}

	chainID, err := infoClient.GetBlockchainID(ctx, builder.Alias)
	if err != nil {
		return nil, err
	}

	asset, err := xChainClient.GetAssetDescription(ctx, "LUX")
	if err != nil {
		return nil, err
	}

	baseTxFee, createAssetTxFee, err := xChainClient.GetTxFee(ctx)
	if err != nil {
		return nil, err
	}

	return &builder.Context{
		NetworkID:        networkID,
		BlockchainID:     chainID,
		LUXAssetID:       asset.AssetID,
		BaseTxFee:        baseTxFee,
		CreateAssetTxFee: createAssetTxFee,
	}, nil
}
