// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/wallet/chain/x/builder"
	"github.com/luxfi/sdk/info"
)

func NewContextFromURI(ctx context.Context, uri string, xAssetID ids.ID, baseTxFee uint64, createAssetTxFee uint64) (*builder.Context, error) {
	infoClient := info.NewClient(uri)
	return NewContextFromClients(ctx, infoClient, xAssetID, baseTxFee, createAssetTxFee)
}

func NewContextFromClients(
	ctx context.Context,
	infoClient *info.Client,
	xAssetID ids.ID,
	baseTxFee uint64,
	createAssetTxFee uint64,
) (*builder.Context, error) {
	networkID, err := infoClient.GetNetworkID(ctx)
	if err != nil {
		return nil, err
	}

	chainID, err := infoClient.GetBlockchainID(ctx, builder.Alias)
	if err != nil {
		return nil, err
	}

	return &builder.Context{
		NetworkID:        networkID,
		BlockchainID:     chainID,
		XAssetID:         xAssetID,
		BaseTxFee:        baseTxFee,
		CreateAssetTxFee: createAssetTxFee,
	}, nil
}
