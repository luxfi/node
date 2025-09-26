// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	stdcontext "context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/api/info"
	"github.com/luxfi/node/vms/xvm"
)

var _ Context = (*pContext)(nil)

type Context interface {
	NetworkID() uint32
	XAssetID() ids.ID
	BaseTxFee() uint64
	CreateNetTxFee() uint64
	TransformNetTxFee() uint64
	CreateBlockchainTxFee() uint64
	AddPrimaryNetworkValidatorFee() uint64
	AddPrimaryNetworkDelegatorFee() uint64
	AddNetValidatorFee() uint64
	AddNetDelegatorFee() uint64
}

type pContext struct {
	networkID                     uint32
	luxAssetID                    ids.ID
	baseTxFee                     uint64
	createSubnetTxFee             uint64
	transformSubnetTxFee          uint64
	createBlockchainTxFee         uint64
	addPrimaryNetworkValidatorFee uint64
	addPrimaryNetworkDelegatorFee uint64
	addNetValidatorFee            uint64
	addSubnetDelegatorFee         uint64
}

func NewContextFromURI(ctx stdcontext.Context, uri string) (Context, error) {
	infoClient := info.NewClient(uri)
	xChainClient := xvm.NewClient(uri, "X")
	return NewContextFromClients(ctx, infoClient, xChainClient)
}

func NewContextFromClients(
	ctx stdcontext.Context,
	infoClient info.Client,
	xChainClient xvm.Client,
) (Context, error) {
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

	return NewContext(
		networkID,
		asset.AssetID,
		uint64(txFees.TxFee),
		uint64(txFees.CreateNetTxFee),
		uint64(txFees.TransformNetTxFee),
		uint64(txFees.CreateBlockchainTxFee),
		uint64(txFees.AddPrimaryNetworkValidatorFee),
		uint64(txFees.AddPrimaryNetworkDelegatorFee),
		uint64(txFees.AddNetValidatorFee),
		uint64(txFees.AddNetDelegatorFee),
	), nil
}

func NewContext(
	networkID uint32,
	luxAssetID ids.ID,
	baseTxFee uint64,
	createSubnetTxFee uint64,
	transformSubnetTxFee uint64,
	createBlockchainTxFee uint64,
	addPrimaryNetworkValidatorFee uint64,
	addPrimaryNetworkDelegatorFee uint64,
	addNetValidatorFee uint64,
	addSubnetDelegatorFee uint64,
) Context {
	return &pContext{
		networkID:                     networkID,
		luxAssetID:                    luxAssetID,
		baseTxFee:                     baseTxFee,
		createSubnetTxFee:             createSubnetTxFee,
		transformSubnetTxFee:          transformSubnetTxFee,
		createBlockchainTxFee:         createBlockchainTxFee,
		addPrimaryNetworkValidatorFee: addPrimaryNetworkValidatorFee,
		addPrimaryNetworkDelegatorFee: addPrimaryNetworkDelegatorFee,
		addNetValidatorFee:            addNetValidatorFee,
		addSubnetDelegatorFee:         addSubnetDelegatorFee,
	}
}

func (c *pContext) NetworkID() uint32 {
	return c.networkID
}

func (c *pContext) XAssetID() ids.ID {
	return c.luxAssetID
}

func (c *pContext) BaseTxFee() uint64 {
	return c.baseTxFee
}

func (c *pContext) CreateNetTxFee() uint64 {
	return c.createSubnetTxFee
}

func (c *pContext) TransformNetTxFee() uint64 {
	return c.transformSubnetTxFee
}

func (c *pContext) CreateBlockchainTxFee() uint64 {
	return c.createBlockchainTxFee
}

func (c *pContext) AddPrimaryNetworkValidatorFee() uint64 {
	return c.addPrimaryNetworkValidatorFee
}

func (c *pContext) AddPrimaryNetworkDelegatorFee() uint64 {
	return c.addPrimaryNetworkDelegatorFee
}

func (c *pContext) AddNetValidatorFee() uint64 {
	return c.addNetValidatorFee
}

func (c *pContext) AddNetDelegatorFee() uint64 {
	return c.addSubnetDelegatorFee
}
