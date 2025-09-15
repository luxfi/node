// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/luxfi/node/staking"
)

func main() {
	// Create staking directory
	stakingDir := filepath.Join(os.Getenv("HOME"), ".luxd", "staking")
	if err := os.MkdirAll(stakingDir, 0700); err != nil {
		fmt.Printf("Failed to create staking directory: %v\n", err)
		os.Exit(1)
	}

	// Generate new certificate and key
	certBytes, keyBytes, err := staking.NewCertAndKeyBytes()
	if err != nil {
		fmt.Printf("Failed to generate cert and key: %v\n", err)
		os.Exit(1)
	}

	// Write certificate
	certPath := filepath.Join(stakingDir, "staker.crt")
	if err := os.WriteFile(certPath, certBytes, 0644); err != nil {
		fmt.Printf("Failed to write certificate: %v\n", err)
		os.Exit(1)
	}

	// Write private key
	keyPath := filepath.Join(stakingDir, "staker.key")
	if err := os.WriteFile(keyPath, keyBytes, 0600); err != nil {
		fmt.Printf("Failed to write private key: %v\n", err)
		os.Exit(1)
	}

	// For BLS signer key, create a placeholder
	// In production this would be a real BLS key
	signerPath := filepath.Join(stakingDir, "signer.key")
	signerKey := []byte("test-signer-key-placeholder")
	if err := os.WriteFile(signerPath, signerKey, 0600); err != nil {
		fmt.Printf("Failed to write signer key: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Staking keys generated successfully in %s\n", stakingDir)
	fmt.Printf("  Certificate: %s\n", certPath)
	fmt.Printf("  Private Key: %s\n", keyPath)
	fmt.Printf("  Signer Key:  %s\n", signerPath)
}