package main

import (
	"fmt"
	"os"

	"github.com/luxfi/crypto/bls"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: blscheck <signer.key path>")
		os.Exit(1)
	}

	keyBytes, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Printf("Error reading key file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Key file size: %d bytes\n", len(keyBytes))
	fmt.Printf("Key bytes (hex): %x\n", keyBytes)

	sk, err := bls.SecretKeyFromBytes(keyBytes)
	if err != nil {
		fmt.Printf("Error parsing secret key: %v\n", err)
		os.Exit(1)
	}

	pk := sk.PublicKey()
	pkBytes := bls.PublicKeyToCompressedBytes(pk)
	fmt.Printf("Derived public key: 0x%x\n", pkBytes)
}
