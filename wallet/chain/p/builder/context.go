// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/api/info"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/xvm"
)

const Alias = "P"

type Context struct {
	NetworkID                     uint32
	LUXAssetID                    ids.ID
	BaseTxFee                     uint64
	CreateSubnetTxFee             uint64
	TransformSubnetTxFee          uint64
	CreateBlockchainTxFee         uint64
	AddPrimaryNetworkValidatorFee uint64
	AddPrimaryNetworkDelegatorFee uint64
	AddSubnetValidatorFee         uint64
	AddSubnetDelegatorFee         uint64
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
		LUXAssetID:                    asset.AssetID,
		BaseTxFee:                     uint64(txFees.TxFee),
		CreateSubnetTxFee:             uint64(txFees.CreateSubnetTxFee),
		TransformSubnetTxFee:          uint64(txFees.TransformSubnetTxFee),
		CreateBlockchainTxFee:         uint64(txFees.CreateBlockchainTxFee),
		AddPrimaryNetworkValidatorFee: uint64(txFees.AddPrimaryNetworkValidatorFee),
		AddPrimaryNetworkDelegatorFee: uint64(txFees.AddPrimaryNetworkDelegatorFee),
		AddSubnetValidatorFee:         uint64(txFees.AddSubnetValidatorFee),
		AddSubnetDelegatorFee:         uint64(txFees.AddSubnetDelegatorFee),
	}, nil
}

func NewConsensusContext(networkID uint32, luxAssetID ids.ID) (context.Context, error) {
	// TODO: This function needs to be refactored to work with standard context.Context
	// For now, return a basic context
	ctx := context.Background()
	return ctx, nil
}
