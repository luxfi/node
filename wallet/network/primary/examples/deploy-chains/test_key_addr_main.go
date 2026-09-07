//go:build ignore

package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/luxfi/crypto/secp256k1"
)

func main() {
	keyStr := strings.TrimSpace(os.Getenv("PRIVATE_KEY"))
	keyBytes, _ := hex.DecodeString(keyStr)
	key, err := secp256k1.ToPrivateKey(keyBytes)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	addr := key.Address()
	fmt.Printf("Address (ShortID): %s\n", addr)
	fmt.Printf("Address (hex): %x\n", addr[:])
	fmt.Printf("Expected hex:  7b8e61041a0691a73924b2bb7169afa6b72e4b54\n")
}
