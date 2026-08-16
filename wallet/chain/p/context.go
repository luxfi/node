// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"context"
	"os"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm"
	"github.com/luxfi/node/vms/platformvm/txs/fee"
	"github.com/luxfi/node/wallet/chain/p/builder"
	"github.com/luxfi/sdk/info"
)

// gasPriceMultiplier increases the gas price to support multiple transactions
// to be issued.
//
// gasPriceMultiplier increases the gas price to allow multiple transactions
// to be issued without waiting for prior ones to leave the mempool.
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

	utxoAssetID, err := chainClient.GetStakingAssetID(ctx, constants.PrimaryNetworkID)
	if err != nil {
		return nil, err
	}

	// LUX_WALLET_UTXO_ASSET_ID_OVERRIDE pins the fee-payment asset to a
	// specific 32-byte asset ID, bypassing the platform.getStakingAssetID
	// result. Required on a network whose live staking asset differs from
	// the legacy LUX asset held by its existing P-chain UTXOs, where the two
	// ids do not agree and the UTXOs are the ones that must be spendable.
	// Empty / unset is a no-op.
	if override := os.Getenv("LUX_WALLET_UTXO_ASSET_ID_OVERRIDE"); override != "" {
		overrideID, perr := ids.FromString(override)
		if perr != nil {
			return nil, perr
		}
		utxoAssetID = overrideID
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
		UTXOAssetID:          utxoAssetID,
		ComplexityWeights: dynamicFeeConfig.Weights,
		GasPrice:          gasPriceMultiplier * gasPrice,
		// Static fee config - use defaults matching platformvm/config
		StaticFeeConfig: fee.StaticConfig{
			TxFee:                  constants.MilliLux,
			CreateAssetTxFee:       10 * constants.MilliLux,
			CreateNetworkTxFee:     constants.Lux,
			CreateChainTxFee:       constants.Lux,
			AddNetworkValidatorFee: 0,
			AddNetworkDelegatorFee: 0,
			AddChainValidatorFee:   constants.MilliLux,
			AddChainDelegatorFee:   constants.MilliLux,
		},
	}, nil
}
