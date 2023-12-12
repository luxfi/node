// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	stdcontext "context"

	"github.com/luxdefi/node/api/info"
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/vms/avm"
)

var _ Context = (*context)(nil)

type Context interface {
	NetworkID() uint32
	BlockchainID() ids.ID
	LUXAssetID() ids.ID
}

type context struct {
	networkID    uint32
	blockchainID ids.ID
	luxAssetID  ids.ID
}

func NewContextFromURI(ctx stdcontext.Context, uri string) (Context, error) {
	infoClient := info.NewClient(uri)
	xChainClient := avm.NewClient(uri, "X")
	return NewContextFromClients(ctx, infoClient, xChainClient)
}

func NewContextFromClients(
	ctx stdcontext.Context,
	infoClient info.Client,
	xChainClient avm.Client,
) (Context, error) {
	networkID, err := infoClient.GetNetworkID(ctx)
	if err != nil {
		return nil, err
	}

	chainID, err := infoClient.GetBlockchainID(ctx, "C")
	if err != nil {
		return nil, err
	}

	asset, err := xChainClient.GetAssetDescription(ctx, "LUX")
	if err != nil {
		return nil, err
	}

	return NewContext(
		networkID,
		chainID,
		asset.AssetID,
	), nil
}

func NewContext(
	networkID uint32,
	blockchainID ids.ID,
	luxAssetID ids.ID,
) Context {
	return &context{
		networkID:    networkID,
		blockchainID: blockchainID,
		luxAssetID:  luxAssetID,
	}
}

func (c *context) NetworkID() uint32 {
	return c.networkID
}

func (c *context) BlockchainID() ids.ID {
	return c.blockchainID
}

func (c *context) LUXAssetID() ids.ID {
	return c.luxAssetID
}
