// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"log"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/wallet/network/primary"
	"github.com/luxfi/node/wallet/network/primary/examples/keyutil"
	"github.com/luxfi/utxo/secp256k1fx"
)

func main() {
	key := keyutil.MustLoadKey()
	uri := primary.LocalAPIURI
	kc := primary.NewKeychainAdapter(secp256k1fx.NewKeychain(key))
	chainOwner := key.Address()

	ctx := context.Background()

	// MakeWallet fetches the available UTXOs owned by [kc] on the network that
	// [uri] is hosting.
	walletSyncStartTime := time.Now()
	wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:         uri,
		LUXKeychain: kc,
		EthKeychain: kc,
	})
	if err != nil {
		log.Fatalf("failed to initialize wallet: %s\n", err)
	}
	log.Printf("synced wallet in %s\n", time.Since(walletSyncStartTime))

	// Get the P-chain wallet
	pWallet := wallet.P()

	// Pull out useful constants to use when issuing transactions.
	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			chainOwner,
		},
	}

	createNetworkStartTime := time.Now()
	createNetworkTx, err := pWallet.IssueCreateNetworkTx(owner)
	if err != nil {
		log.Fatalf("failed to issue create network transaction: %s\n", err)
	}
	log.Printf("created new network %s in %s\n", createNetworkTx.ID(), time.Since(createNetworkStartTime))
}
