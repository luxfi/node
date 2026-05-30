//go:build ignore

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/node/wallet/network/primary"
	"github.com/luxfi/utxo/secp256k1fx"
)

func debugBalance() {
	uri := "http://24.199.71.30:9650"
	keyHex := "03eccfbc56d951d9a9e19db9486fae39ece158f3a748e94fadbeee91ff3aae5f"
	keyBytes, _ := hex.DecodeString(keyHex)
	key, err := secp256k1.ToPrivateKey(keyBytes)
	if err != nil {
		log.Fatal("key error:", err)
	}
	_ = strings.TrimSpace

	addr := key.Address()
	fmt.Printf("Key ShortID hex: %x\n", addr[:])

	kc := primary.NewKeychainAdapter(secp256k1fx.NewKeychain(key))
	addrs := kc.Addresses()
	fmt.Printf("Keychain has %d addresses\n", addrs.Len())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	state, err := primary.FetchState(ctx, uri, addrs)
	if err != nil {
		log.Fatal("FetchState error: ", err)
	}
	fmt.Printf("NetworkID: %d\n", state.PCTX.NetworkID)
	fmt.Printf("UTXOAssetID: %s\n", state.PCTX.UTXOAssetID)

	wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:         uri,
		LUXKeychain: kc,
		EVMKeychain: kc,
	})
	if err != nil {
		log.Fatal("MakeWallet error: ", err)
	}

	luxAssetID := wallet.X().Builder().Context().UTXOAssetID
	pBal, _ := wallet.P().Builder().GetBalance()
	fmt.Printf("P balance: %v\n", pBal)
	fmt.Printf("P LUX: %d\n", pBal[luxAssetID])
}
