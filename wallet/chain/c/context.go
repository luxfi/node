// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/api/info"
	"github.com/luxfi/node/v2/quasar"
	"github.com/luxfi/node/v2/utils/constants"
	log "github.com/luxfi/log"
	"github.com/luxfi/node/v2/vms/xvm"
)

const Alias = "C"

type Context struct {
	NetworkID    uint32
	BlockchainID ids.ID
	LUXAssetID   ids.ID
}

func NewContextFromURI(ctx context.Context, uri string) (*Context, error) {
	infoClient := info.NewClient(uri)
	xChainClient := xvm.NewClient(uri, "X")
	return NewContextFromClients(ctx, infoClient, xChainClient)
}

func NewContextFromClients(
	ctx context.Context,
	infoClient *info.Client,
	xChainClient *xvm.Client,
) (*Context, error) {
	networkID, err := infoClient.GetNetworkID(ctx)
	if err != nil {
		return nil, err
	}

	blockchainID, err := infoClient.GetBlockchainID(ctx, Alias)
	if err != nil {
		return nil, err
	}

	luxAsset, err := xChainClient.GetAssetDescription(ctx, "LUX")
	if err != nil {
		return nil, err
	}

	return &Context{
		NetworkID:    networkID,
		BlockchainID: blockchainID,
		LUXAssetID:   luxAsset.AssetID,
	}, nil
}

func newLinearContext(c *Context) (*quasar.Context, error) {
	lookup := ids.NewAliaser()
	return &quasar.Context{
		NetworkID:  c.NetworkID,
		SubnetID:   constants.PrimaryNetworkID,
		ChainID:    c.BlockchainID,
		CChainID:   c.BlockchainID,
		LUXAssetID: c.LUXAssetID,
		Log:        log.NewNoOpLogger(),
		BCLookup:   lookup,
	}, lookup.Alias(c.BlockchainID, Alias)
}
