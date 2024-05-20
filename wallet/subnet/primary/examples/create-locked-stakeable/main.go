// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"log"
	"time"

	"github.com/luxfi/node/genesis"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/formatting/address"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/subnet/primary"
)

func main() {
	key := genesis.EWOQKey
	uri := primary.LocalAPIURI
	kc := secp256k1fx.NewKeychain(key)
	amount := 500 * units.MilliLux
	locktime := uint64(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).Unix())
	destAddrStr := "P-local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"

	destAddr, err := address.ParseToID(destAddrStr)
	if err != nil {
		log.Fatalf("failed to parse address: %s\n", err)
	}

	ctx := context.Background()

	// MakeWallet fetches the available UTXOs owned by [kc] on the network that
	// [uri] is hosting.
	walletSyncStartTime := time.Now()
	wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:          uri,
		LUXKeychain: kc,
		EthKeychain:  kc,
	})
	if err != nil {
		log.Fatalf("failed to initialize wallet: %s\n", err)
	}
	log.Printf("synced wallet in %s\n", time.Since(walletSyncStartTime))

	// Get the P-chain wallet
	pWallet := wallet.P()
	luxAssetID := pWallet.LUXAssetID()

	issueTxStartTime := time.Now()
	tx, err := pWallet.IssueBaseTx([]*lux.TransferableOutput{
		{
			Asset: lux.Asset{
				ID: luxAssetID,
			},
			Out: &stakeable.LockOut{
				Locktime: locktime,
				TransferableOut: &secp256k1fx.TransferOutput{
					Amt: amount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs: []ids.ShortID{
							destAddr,
						},
					},
				},
			},
		},
	})
	if err != nil {
		log.Fatalf("failed to issue transaction: %s\n", err)
	}
	log.Printf("issued %s in %s\n", tx.ID(), time.Since(issueTxStartTime))
}
