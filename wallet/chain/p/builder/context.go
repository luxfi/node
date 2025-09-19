// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/api/info"
	"github.com/luxfi/node/vms/xvm"
)

const Alias = "P"

type Context struct {
	NetworkID                     uint32
	BlockchainID                  ids.ID // Added for test compatibility
	LUXAssetID                    ids.ID
	BaseTxFee                     uint64
	CreateNetTxFee                uint64
	TransformNetTxFee             uint64
	CreateBlockchainTxFee         uint64
	AddPrimaryNetworkValidatorFee uint64
	AddPrimaryNetworkDelegatorFee uint64
	AddNetValidatorFee            uint64
	AddNetDelegatorFee            uint64
}

func NewContextFromURI(ctx context.Context, uri string) (*Context, error) {
	infoClient := info.NewClient(uri)
	xChainClient := xvm.NewClient(uri, "X")
	return NewContextFromClients(ctx, infoClient, xChainClient)
}

func NewContextFromClients(
	ctx context.Context,
	infoClient info.Client,
	xChainClient xvm.Client,
) (*Context, error) {
	networkID, err := infoClient.GetNetworkID(ctx)
	if err != nil {
		return nil, err
	}

	asset, err := xChainClient.GetAssetDescription(ctx, "LUX")
	if err != nil {
		return nil, err
	}

	txFees, err := infoClient.GetTxFee(ctx)
	if err != nil {
		return nil, err
	}

	return &Context{
		NetworkID:                     networkID,
		BlockchainID:                  ids.Empty, // Default to PlatformChainID (constants.PlatformChainID)
		LUXAssetID:                    asset.AssetID,
		BaseTxFee:                     uint64(txFees.TxFee),
		CreateNetTxFee:                uint64(txFees.CreateNetTxFee),
		TransformNetTxFee:             uint64(txFees.TransformNetTxFee),
		CreateBlockchainTxFee:         uint64(txFees.CreateBlockchainTxFee),
		AddPrimaryNetworkValidatorFee: uint64(txFees.AddPrimaryNetworkValidatorFee),
		AddPrimaryNetworkDelegatorFee: uint64(txFees.AddPrimaryNetworkDelegatorFee),
		AddNetValidatorFee:            uint64(txFees.AddNetValidatorFee),
		AddNetDelegatorFee:            uint64(txFees.AddNetDelegatorFee),
	}, nil
}

func NewConsensusContext(networkID uint32, luxAssetID ids.ID) (context.Context, error) {
	// For now, return a basic context
	ctx := context.Background()
	return ctx, nil
}
