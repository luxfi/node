// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

// Package keyutil provides utilities for loading private keys from environment
// variables or files for use in example programs.
package keyutil

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/luxfi/crypto/secp256k1"
)

// MustLoadKey loads a secp256k1 private key from:
// 1. LUX_PRIVATE_KEY environment variable (hex-encoded, with or without 0x prefix)
// 2. File path provided as first command line argument
//
// Panics with a helpful message if no key is provided.
func MustLoadKey() *secp256k1.PrivateKey {
	key, err := LoadKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading private key: %s\n", err)
		fmt.Fprintf(os.Stderr, "\nUsage:\n")
		fmt.Fprintf(os.Stderr, "  Set LUX_PRIVATE_KEY environment variable (hex-encoded)\n")
		fmt.Fprintf(os.Stderr, "  Or pass key file path as first argument\n")
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  export LUX_PRIVATE_KEY=0x56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027\n")
		fmt.Fprintf(os.Stderr, "  %s\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nOr:\n")
		fmt.Fprintf(os.Stderr, "  %s /path/to/keyfile.hex\n", os.Args[0])
		os.Exit(1)
	}
	return key
}

// LoadKey attempts to load a private key from environment or file.
func LoadKey() (*secp256k1.PrivateKey, error) {
	// Try environment variable first
	keyStr := os.Getenv("LUX_PRIVATE_KEY")
	if keyStr != "" {
		return parseHexKey(keyStr)
	}

	// Try file path from arguments
	if len(os.Args) > 1 {
		keyPath := os.Args[1]
		data, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read key file %q: %w", keyPath, err)
		}
		return parseHexKey(string(data))
	}

	return nil, fmt.Errorf("no private key provided: set LUX_PRIVATE_KEY or pass key file path")
}

// parseHexKey parses a hex-encoded private key string.
func parseHexKey(s string) (*secp256k1.PrivateKey, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")

	keyBytes, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid hex key: %w", err)
	}

	if len(keyBytes) != secp256k1.PrivateKeyLen {
		return nil, fmt.Errorf("invalid key length: got %d bytes, want %d", len(keyBytes), secp256k1.PrivateKeyLen)
	}

	key, err := secp256k1.ToPrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}
	return key, nil
}
