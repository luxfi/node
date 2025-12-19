package main

import (
	"encoding/hex"
	"fmt"
	"os"

	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/node/utils/formatting/address"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: keytoaddr <hex_private_key>")
		os.Exit(1)
	}

	keyHex := os.Args[1]
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		fmt.Printf("Error decoding hex: %v\n", err)
		os.Exit(1)
	}

	sk, err := secp256k1.ToPrivateKey(keyBytes)
	if err != nil {
		fmt.Printf("Error creating key: %v\n", err)
		os.Exit(1)
	}

	pk := sk.PublicKey()
	addr := pk.Address()

	// Format as P-chain address with "lux" HRP (network ID 96369)
	pAddr, err := address.Format("P", "lux", addr[:])
	if err != nil {
		fmt.Printf("Error formatting address: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(pAddr)
}
