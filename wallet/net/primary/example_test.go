// Copyright (C) 2019-2025, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"
	"log"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/genesis"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/secp256k1fx"
)

func ExampleWallet() {
	ctx := context.Background()
	kc := secp256k1fx.NewKeychain(genesis.EWOQKey)

	// MakeWallet fetches the available UTXOs owned by [kc] on the network that
	// [LocalAPIURI] is hosting.
	walletSyncStartTime := time.Now()
	wallet, err := MakeWallet(ctx, &WalletConfig{
		URI:         LocalAPIURI,
		LUXKeychain: kc,
		EthKeychain: kc,
	})
	if err != nil {
		log.Fatalf("failed to initialize wallet with: %s\n", err)
		return
	}
	log.Printf("synced wallet in %s\n", time.Since(walletSyncStartTime))

	// Get the P-chain and the X-chain wallets
	pWallet := wallet.P()
	xWallet := wallet.X()
	xBuilder := xWallet.Builder()
	xContext := xBuilder.Context()

	// Pull out useful constants to use when issuing transactions.
	xChainID := xContext.BlockchainID
	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			genesis.EWOQKey.PublicKey().Address(),
		},
	}

	// Create a custom asset to send to the P-chain.
	createAssetStartTime := time.Now()
	createAssetTx, err := xWallet.IssueCreateAssetTx(
		"RnM",
		"RNM",
		9,
		map[uint32][]verify.State{
			0: {
				&secp256k1fx.TransferOutput{
					Amt:          100 * units.MegaLux,
					OutputOwners: *owner,
				},
			},
		},
	)
	if err != nil {
		log.Fatalf("failed to create new X-chain asset with: %s\n", err)
		return
	}
	createAssetTxID := createAssetTx.ID()
	log.Printf("created X-chain asset %s in %s\n", createAssetTxID, time.Since(createAssetStartTime))

	// Send 100 MegaLux to the P-chain.
	exportStartTime := time.Now()
	exportTx, err := xWallet.IssueExportTx(
		constants.PlatformChainID,
		[]*lux.TransferableOutput{
			{
				Asset: lux.Asset{
					ID: createAssetTxID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt:          100 * units.MegaLux,
					OutputOwners: *owner,
				},
			},
		},
	)
	if err != nil {
		log.Fatalf("failed to issue X->P export transaction with: %s\n", err)
		return
	}
	exportTxID := exportTx.ID()
	log.Printf("issued X->P export %s in %s\n", exportTxID, time.Since(exportStartTime))

	// Import the 100 MegaLux from the X-chain into the P-chain.
	importStartTime := time.Now()
	importTx, err := pWallet.IssueImportTx(xChainID, owner)
	if err != nil {
		log.Fatalf("failed to issue X->P import transaction with: %s\n", err)
		return
	}
	importTxID := importTx.ID()
	log.Printf("issued X->P import %s in %s\n", importTxID, time.Since(importStartTime))

	createSubnetStartTime := time.Now()
	createSubnetTx, err := pWallet.IssueCreateNetTx(owner)
	if err != nil {
		log.Fatalf("failed to issue create net transaction with: %s\n", err)
		return
	}
	createSubnetTxID := createSubnetTx.ID()
	log.Printf("issued create net transaction %s in %s\n", createSubnetTxID, time.Since(createSubnetStartTime))

	transformSubnetStartTime := time.Now()
	transformSubnetTx, err := pWallet.IssueTransformNetTx(
		createSubnetTxID,
		createAssetTxID,
		50*units.MegaLux,
		100*units.MegaLux,
		reward.PercentDenominator,
		reward.PercentDenominator,
		1,
		100*units.MegaLux,
		time.Second,
		365*24*time.Hour,
		0,
		1,
		5,
		.80*reward.PercentDenominator,
	)
	if err != nil {
		log.Fatalf("failed to issue transform net transaction with: %s\n", err)
		return
	}
	transformSubnetTxID := transformSubnetTx.ID()
	log.Printf("issued transform net transaction %s in %s\n", transformSubnetTxID, time.Since(transformSubnetStartTime))

	addPermissionlessValidatorStartTime := time.Now()
	startTime := time.Now().Add(time.Minute)
	addNetValidatorTx, err := pWallet.IssueAddPermissionlessValidatorTx(
		&txs.NetValidator{
			Validator: txs.Validator{
				NodeID: genesis.LocalConfig.InitialStakers[0].NodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(startTime.Add(5 * time.Second).Unix()),
				Wght:   25 * units.MegaLux,
			},
			Net: createSubnetTxID,
		},
		&signer.Empty{},
		createAssetTx.ID(),
		&secp256k1fx.OutputOwners{},
		&secp256k1fx.OutputOwners{},
		reward.PercentDenominator,
	)
	if err != nil {
		log.Fatalf("failed to issue add net validator with: %s\n", err)
		return
	}
	addNetValidatorTxID := addNetValidatorTx.ID()
	log.Printf("issued add net validator transaction %s in %s\n", addNetValidatorTxID, time.Since(addPermissionlessValidatorStartTime))

	addPermissionlessDelegatorStartTime := time.Now()
	addSubnetDelegatorTx, err := pWallet.IssueAddPermissionlessDelegatorTx(
		&txs.NetValidator{
			Validator: txs.Validator{
				NodeID: genesis.LocalConfig.InitialStakers[0].NodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(startTime.Add(5 * time.Second).Unix()),
				Wght:   25 * units.MegaLux,
			},
			Net: createSubnetTxID,
		},
		createAssetTxID,
		&secp256k1fx.OutputOwners{},
	)
	if err != nil {
		log.Fatalf("failed to issue add net delegator with: %s\n", err)
		return
	}
	addSubnetDelegatorTxID := addSubnetDelegatorTx.ID()
	log.Printf("issued add net validator delegator %s in %s\n", addSubnetDelegatorTxID, time.Since(addPermissionlessDelegatorStartTime))
}
