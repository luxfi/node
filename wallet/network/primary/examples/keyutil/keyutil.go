// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package keyutil provides utilities for loading private keys that integrate
// with the Lux CLI key management system (~/.lux/keys/).
package keyutil

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/go-bip32"
	"github.com/luxfi/go-bip39"
	"github.com/luxfi/keys"
)

const (
	// LuxKeysDir is the CLI key storage directory
	LuxKeysDir = ".lux/keys"
	// MnemonicFile is the mnemonic filename used by CLI
	MnemonicFile = "mnemonic.txt"
	// ECPrivateKeyFile is the EC private key filename used by CLI
	ECPrivateKeyFile = "ec/private.key"
)

// MustLoadKey loads a secp256k1 private key from (in order of priority):
//  1. MNEMONIC environment variable (BIP-39 mnemonic phrase)
//  2. Liquid KMS via native ZAP — when KMS_ADDR + KMS_ENV +
//     KMS_MNEMONIC_PATH are set in the environment. Uses the canonical
//     luxfi/kms keys.LoadMnemonic loader so every Lux-derived
//     service resolves keys the same way.
//  3. Key name provided as first command-line argument
//  4. ~/.lux/keys/default/ if it exists
//
// Panics with a helpful message if no key is provided.
func MustLoadKey() *secp256k1.PrivateKey {
	key, err := LoadKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading private key: %s\n", err)
		fmt.Fprintf(os.Stderr, "\nUsage (in order of priority):\n")
		fmt.Fprintf(os.Stderr, "  1. Set MNEMONIC env var (BIP-39 mnemonic)\n")
		fmt.Fprintf(os.Stderr, "  2. Set KMS_ADDR + KMS_ENV + KMS_MNEMONIC_PATH (native ZAP)\n")
		fmt.Fprintf(os.Stderr, "  3. Pass key name as argument: %s <key-name>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nAvailable keys in ~/.lux/keys/:\n")
		listAvailableKeys()
		fmt.Fprintf(os.Stderr, "\nCreate keys with: lux key create <name>\n")
		os.Exit(1)
	}
	return key
}

// LoadKey attempts to load a private key using the priority order above.
func LoadKey() (*secp256k1.PrivateKey, error) {
	// 1. MNEMONIC env var — local dev + CI test seam.
	if mnemonic := strings.TrimSpace(os.Getenv("MNEMONIC")); mnemonic != "" {
		return keyFromMnemonic(mnemonic)
	}

	// 2. Liquid KMS via native ZAP — production path for any Lux-derived
	// service running under a KMS-projected env. The canonical loader
	// lives in luxfi/keys (alongside the BIP-39 derivation primitives)
	// so luxd, netrunner, lux/cli, and every descending L1's bootstrap
	// all resolve mnemonics the same way.
	//
	// Consensus-native auth (KMS-side gate flipped 2026-05-30): every
	// secret-opcode envelope MUST carry a signed identity. The dial
	// derives a *keys.ServiceIdentity from KMS_BOOTSTRAP_MNEMONIC under
	// the well-known service path "luxd/staking-bootstrap"; production
	// operators provision this bootstrap mnemonic out-of-band (sealed
	// envelope, hardware token unwrap, etc.) so the on-disk staking
	// material itself comes from the KMS-held operational mnemonic.
	if addr := os.Getenv("KMS_ADDR"); addr != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		identity, err := bootstrapIdentity("luxd/staking-bootstrap")
		if err != nil {
			return nil, fmt.Errorf("derive KMS dial identity: %w", err)
		}
		defer identity.Wipe()
		mnemonic, err := keys.LoadMnemonicFromKMS(ctx, addr,
			os.Getenv("KMS_ENV"),
			envOr("KMS_MNEMONIC_PATH", "/mnemonic"),
			identity)
		if err != nil {
			return nil, fmt.Errorf("load mnemonic from KMS: %w", err)
		}
		return keyFromMnemonic(mnemonic)
	}

	// 3. Key name from command-line arguments.
	if len(os.Args) > 1 {
		keyName := os.Args[1]
		if key, err := LoadKeyByName(keyName); err == nil {
			return key, nil
		}
		// Try as a file path
		if data, err := os.ReadFile(keyName); err == nil {
			return parseKeyData(data)
		}
	}

	// 4. Default key in ~/.lux/keys/default/
	if key, err := LoadKeyByName("default"); err == nil {
		return key, nil
	}

	return nil, fmt.Errorf("no private key provided")
}

// envOr returns the value of env var `name` if set + non-empty, else def.
func envOr(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

// bootstrapIdentity derives the *keys.ServiceIdentity used to sign the
// initial KMS dial envelope. The bootstrap mnemonic comes from (in
// order):
//
//  1. KMS_BOOTSTRAP_MNEMONIC env var — explicit operator override.
//  2. MNEMONIC env var — local dev + CI test seam; the same mnemonic
//     used for staking material derivation is reused for the dial.
//
// At least one MUST be set when the KMS dial path is engaged. Empty
// or invalid bootstrap mnemonic returns an error so the dial never
// reaches the KMS server with nil identity.
//
// Identity derivation is the canonical luxfi/keys.NewServiceIdentity
// path: the bootstrap mnemonic + servicePath fold deterministically
// into an ML-DSA-65 NodeID via BIP-32 + SHAKE-256.
func bootstrapIdentity(servicePath string) (*keys.ServiceIdentity, error) {
	m := strings.TrimSpace(os.Getenv("KMS_BOOTSTRAP_MNEMONIC"))
	if m == "" {
		m = strings.TrimSpace(os.Getenv("MNEMONIC"))
	}
	if m == "" {
		return nil, fmt.Errorf("KMS_BOOTSTRAP_MNEMONIC (or MNEMONIC) must be set to dial KMS")
	}
	return keys.NewServiceIdentity(m, servicePath)
}

// LoadKeyByName loads a key from ~/.lux/keys/<name>/
// It first tries the EC private key file, then falls back to mnemonic
func LoadKeyByName(name string) (*secp256k1.PrivateKey, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	baseDir := filepath.Join(home, LuxKeysDir, name)

	// Try EC private key first (faster, no derivation needed)
	ecKeyPath := filepath.Join(baseDir, ECPrivateKeyFile)
	if data, err := os.ReadFile(ecKeyPath); err == nil {
		return parseHexKey(string(data))
	}

	// Fall back to mnemonic
	mnemonicPath := filepath.Join(baseDir, MnemonicFile)
	if data, err := os.ReadFile(mnemonicPath); err == nil {
		return keyFromMnemonic(string(data))
	}

	// Try as legacy .pk file (older format)
	pkPath := filepath.Join(home, LuxKeysDir, name+".pk")
	if data, err := os.ReadFile(pkPath); err == nil {
		return parseHexKey(string(data))
	}

	return nil, fmt.Errorf("key %q not found in ~/.lux/keys/", name)
}

// keyFromMnemonic derives a private key from a BIP39 mnemonic phrase.
func keyFromMnemonic(mnemonic string) (*secp256k1.PrivateKey, error) {
	mnemonic = strings.TrimSpace(mnemonic)
	words := strings.Fields(mnemonic)
	if len(words) < 12 {
		return nil, fmt.Errorf("invalid mnemonic: need at least 12 words, got %d", len(words))
	}

	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, fmt.Errorf("invalid BIP39 mnemonic")
	}

	// Generate seed from mnemonic (no passphrase)
	seed := bip39.NewSeed(mnemonic, "")

	// BIP44 derivation: m/44'/60'/0'/0/0
	masterKey, err := bip32.NewMasterKey(seed)
	if err != nil {
		return nil, fmt.Errorf("failed to create master key: %w", err)
	}

	purpose, err := masterKey.NewChildKey(bip32.FirstHardenedChild + 44)
	if err != nil {
		return nil, fmt.Errorf("failed to derive purpose: %w", err)
	}

	coinType, err := purpose.NewChildKey(bip32.FirstHardenedChild + 60)
	if err != nil {
		return nil, fmt.Errorf("failed to derive coin type: %w", err)
	}

	account, err := coinType.NewChildKey(bip32.FirstHardenedChild + 0)
	if err != nil {
		return nil, fmt.Errorf("failed to derive account: %w", err)
	}

	change, err := account.NewChildKey(0)
	if err != nil {
		return nil, fmt.Errorf("failed to derive change: %w", err)
	}

	// Default address index is 0 (m/44'/60'/0'/0/0). Callers that need a
	// non-default address use BIP39 with a different account word path.
	childKey, err := change.NewChildKey(0)
	if err != nil {
		return nil, fmt.Errorf("failed to derive address index 0: %w", err)
	}

	return secp256k1.ToPrivateKey(childKey.Key)
}

// parseKeyData tries to parse key data as hex or mnemonic
func parseKeyData(data []byte) (*secp256k1.PrivateKey, error) {
	s := strings.TrimSpace(string(data))

	// If it looks like a mnemonic (contains spaces, multiple words)
	if strings.Contains(s, " ") && len(strings.Fields(s)) >= 12 {
		return keyFromMnemonic(s)
	}

	// Try as hex
	return parseHexKey(s)
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

	return secp256k1.ToPrivateKey(keyBytes)
}

// listAvailableKeys prints available keys in ~/.lux/keys/
func listAvailableKeys() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	keysDir := filepath.Join(home, LuxKeysDir)
	entries, err := os.ReadDir(keysDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  (could not read keys directory)\n")
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Check if it has mnemonic or ec key
			baseDir := filepath.Join(keysDir, entry.Name())
			hasMnemonic := fileExists(filepath.Join(baseDir, MnemonicFile))
			hasEC := fileExists(filepath.Join(baseDir, ECPrivateKeyFile))
			if hasMnemonic || hasEC {
				fmt.Fprintf(os.Stderr, "  - %s\n", entry.Name())
			}
		} else if strings.HasSuffix(entry.Name(), ".pk") {
			// Legacy .pk file
			name := strings.TrimSuffix(entry.Name(), ".pk")
			fmt.Fprintf(os.Stderr, "  - %s (legacy)\n", name)
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
