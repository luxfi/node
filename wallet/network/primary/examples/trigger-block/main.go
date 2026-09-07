// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// trigger-block: submits a P-chain transfer to force block production on testnet
package main

import (
	"context"
	"encoding/hex"
	"log"
	"os"
	"strings"
	"time"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/node/wallet/network/primary"
	"github.com/luxfi/utxo/secp256k1fx"

	"github.com/luxfi/crypto/secp256k1"
)

func main() {
	testnetURI := "http://127.0.0.1:19642" // point at an active validator with enough peers to reach quorum

	// Load fee0 key
	keyHex := strings.TrimSpace(string(mustRead("~/.lux/keys/fee0/ec/private.key")))
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		log.Fatalf("invalid key hex: %v", err)
	}
	privKey, err := secp256k1.ToPrivateKey(keyBytes)
	if err != nil {
		log.Fatalf("failed to create private key: %v", err)
	}
	kc := secp256k1fx.NewKeychain(privKey)
	pKC := primary.NewKeychainAdapter(kc)

	ctx := context.Background()

	// Sync wallet from testnet
	log.Printf("syncing wallet from %s...", testnetURI)
	wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:         testnetURI,
		LUXKeychain: pKC,
		EVMKeychain: pKC,
	})
	if err != nil {
		log.Fatalf("failed to make wallet: %v", err)
	}

	pWallet := wallet.P()
	pBuilder := pWallet.Builder()
	pCtx := pBuilder.Context()
	utxoAssetID := pCtx.UTXOAssetID

	// Check balance
	balances, err := pBuilder.GetBalance()
	if err != nil {
		log.Fatalf("failed to get balance: %v", err)
	}
	luxBalance := balances[utxoAssetID]
	log.Printf("P-chain LUX balance: %d µLUX = %d LUX", luxBalance, luxBalance/constants.Lux)

	// Send 1 LUX to self — this forces block production on P-chain
	selfAddr := privKey.Address()
	amount := uint64(1) * constants.Lux

	log.Printf("sending %d LUX to self (%s) to trigger block production...", amount/constants.Lux, selfAddr)

	start := time.Now()
	tx, err := pWallet.IssueBaseTx(
		[]*lux.TransferableOutput{
			{
				Asset: lux.Asset{ID: utxoAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: amount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{selfAddr},
					},
				},
			},
		},
	)
	if err != nil {
		log.Fatalf("failed to issue BaseTx: %v", err)
	}
	log.Printf("SUCCESS: issued BaseTx txID=%s (took %v)", tx.ID(), time.Since(start))
}

func mustRead(path string) []byte {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = home + path[1:]
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("failed to read %s: %v", path, err)
	}
	return data
}
