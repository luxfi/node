// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"
	
	"github.com/luxfi/node/utils/crypto/bls/signer/localsigner"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: gen_bls_key <output_path>")
		os.Exit(1)
	}
	
	outputPath := os.Args[1]
	
	signer, err := localsigner.New()
	if err != nil {
		fmt.Printf("Error generating BLS key: %v\n", err)
		os.Exit(1)
	}
	
	skBytes := signer.ToBytes()
	err = os.WriteFile(outputPath, skBytes, 0600)
	if err != nil {
		fmt.Printf("Error writing key: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("Generated BLS key successfully to %s\n", outputPath)
	fmt.Printf("Key size: %d bytes\n", len(skBytes))
}
