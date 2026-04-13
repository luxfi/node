//go:build ignore

package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/wallet/network/primary"
	"github.com/luxfi/node/wallet/network/primary/examples/keyutil"
	"github.com/luxfi/utxo/secp256k1fx"
)

func main() {
	uri := os.Getenv("LUX_URI")
	if uri == "" {
		uri = "https://api.lux.network"
	}

	key := keyutil.MustLoadKey()
	kc := primary.NewKeychainAdapter(secp256k1fx.NewKeychain(key))
	log.Printf("URI: %s", uri)
	log.Printf("Key: %s", key.Address())

	ctx := context.Background()

	log.Printf("Syncing wallet...")
	wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:         uri,
		LUXKeychain: kc,
		EthKeychain: kc,
	})
	if err != nil {
		log.Fatalf("MakeWallet: %s", err)
	}
	log.Printf("Wallet synced")

	// Export 2000 LUX from C-Chain to P-Chain (enough for validator stake + fees)
	amount := uint64(2_000) * constants.Lux
	pChainID := ids.Empty // P-Chain

	cWallet := wallet.C()
	log.Printf("Exporting %d LUX from C-Chain to P-Chain...", amount/constants.Lux)

	exportStartTime := time.Now()
	exportTx, err := cWallet.IssueExportTx(
		pChainID,
		[]*secp256k1fx.TransferOutput{{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{key.Address()},
			},
		}},
	)
	if err != nil {
		log.Fatalf("ExportTx: %s", err)
	}
	log.Printf("Export TX: %s (took %s)", exportTx.ID(), time.Since(exportStartTime))

	// Import on P-Chain
	pWallet := wallet.P()
	log.Printf("Importing on P-Chain...")
	importStartTime := time.Now()
	importTx, err := pWallet.IssueImportTx(
		wallet.C().Builder().Context().BlockchainID,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{key.Address()},
		},
	)
	if err != nil {
		log.Fatalf("ImportTx: %s", err)
	}
	log.Printf("Import TX: %s (took %s)", importTx.ID(), time.Since(importStartTime))
	log.Printf("SUCCESS: 2000 LUX transferred C-Chain -> P-Chain")
}
