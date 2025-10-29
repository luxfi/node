// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/tyler-smith/go-bip32"
	"github.com/tyler-smith/go-bip39"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/staking"
)

func main() {
	mnemonic := flag.String("mnemonic", "", "BIP39 mnemonic phrase")
	startIndex := flag.Int("start", 0, "Starting account index")
	count := flag.Int("count", 5, "Number of validators to generate")
	outputDir := flag.String("output", "", "Output directory for validator keys")
	network := flag.String("network", "mainnet", "Network name (mainnet or testnet)")
	flag.Parse()

	if *mnemonic == "" {
		fmt.Println("Error: mnemonic is required")
		flag.Usage()
		os.Exit(1)
	}

	if *outputDir == "" {
		fmt.Println("Error: output directory is required")
		flag.Usage()
		os.Exit(1)
	}

	// Validate mnemonic
	if !bip39.IsMnemonicValid(*mnemonic) {
		fmt.Println("Error: invalid mnemonic phrase")
		os.Exit(1)
	}

	// Generate seed from mnemonic
	seed := bip39.NewSeed(*mnemonic, "")

	// Derive master key
	masterKey, err := bip32.NewMasterKey(seed)
	if err != nil {
		fmt.Printf("Error creating master key: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generating %d validator keys for %s (accounts %d-%d)\n", 
		*count, *network, *startIndex, *startIndex+*count-1)
	fmt.Println()

	for i := 0; i < *count; i++ {
		accountIndex := uint32(*startIndex + i)
		
		// BIP44 path: m/44'/9000'/0'/0/accountIndex
		key := masterKey
		
		// m/44'
		key, err = key.NewChildKey(bip32.FirstHardenedChild + 44)
		if err != nil {
			fmt.Printf("Error deriving purpose: %v\n", err)
			continue
		}
		
		// m/44'/9000'
		key, err = key.NewChildKey(bip32.FirstHardenedChild + 9000)
		if err != nil {
			fmt.Printf("Error deriving coin type: %v\n", err)
			continue
		}
		
		// m/44'/9000'/0'
		key, err = key.NewChildKey(bip32.FirstHardenedChild + 0)
		if err != nil {
			fmt.Printf("Error deriving account: %v\n", err)
			continue
		}
		
		// m/44'/9000'/0'/0
		key, err = key.NewChildKey(0)
		if err != nil {
			fmt.Printf("Error deriving change: %v\n", err)
			continue
		}
		
		// m/44'/9000'/0'/0/accountIndex
		key, err = key.NewChildKey(accountIndex)
		if err != nil {
			fmt.Printf("Error deriving address index: %v\n", err)
			continue
		}

		// Convert to ECDSA private key
		privKey := bytesToPrivateKey(key.Key)

		// Create validator directory
		validatorDir := filepath.Join(*outputDir, fmt.Sprintf("node%d", i+1))
		stakingDir := filepath.Join(validatorDir, "staking")
		if err := os.MkdirAll(stakingDir, 0700); err != nil {
			fmt.Printf("Error creating directory %s: %v\n", stakingDir, err)
			continue
		}

		// Generate certificate from the derived private key
		certBytes, keyBytes, err := createCertAndKeyBytes(privKey)
		if err != nil {
			fmt.Printf("Error generating certificate: %v\n", err)
			continue
		}

		// Write certificate
		certPath := filepath.Join(stakingDir, "staker.crt")
		if err := os.WriteFile(certPath, certBytes, 0644); err != nil {
			fmt.Printf("Error writing certificate: %v\n", err)
			continue
		}

		// Write private key
		keyPath := filepath.Join(stakingDir, "staker.key")
		if err := os.WriteFile(keyPath, keyBytes, 0600); err != nil {
			fmt.Printf("Error writing private key: %v\n", err)
			continue
		}

		// Generate BLS signer key (deterministic from same seed)
		signerPath := filepath.Join(stakingDir, "signer.key")
		blsKey := key.Key // Use derived key for BLS too
		if err := os.WriteFile(signerPath, blsKey, 0600); err != nil {
			fmt.Printf("Error writing signer key: %v\n", err)
			continue
		}

		// Calculate NodeID
		cert, err := staking.LoadTLSCertFromBytes(keyBytes, certBytes)
		if err != nil {
			fmt.Printf("Error loading certificate: %v\n", err)
			continue
		}
		
		// Convert x509.Certificate to staking.Certificate
		stakingCert := &staking.Certificate{
			Raw:       cert.Leaf.Raw,
			PublicKey: cert.Leaf.PublicKey,
		}
		nodeID := ids.NodeIDFromCert(&ids.Certificate{
			Raw:       stakingCert.Raw,
			PublicKey: stakingCert.PublicKey,
		})

		fmt.Printf("Validator %d (account %d):\n", i+1, accountIndex)
		fmt.Printf("  NodeID:      %s\n", nodeID)
		fmt.Printf("  Directory:   %s\n", validatorDir)
		fmt.Printf("  Certificate: %s\n", certPath)
		fmt.Printf("  Private Key: %s\n", keyPath)
		fmt.Printf("  BLS Key:     %s\n", signerPath)
		fmt.Println()
	}

	fmt.Println("✓ All validator keys generated successfully")
}

func bytesToPrivateKey(keyBytes []byte) *ecdsa.PrivateKey {
	curve := elliptic.P256()
	privKey := new(ecdsa.PrivateKey)
	privKey.D = new(big.Int).SetBytes(keyBytes)
	privKey.PublicKey.Curve = curve
	privKey.PublicKey.X, privKey.PublicKey.Y = curve.ScalarBaseMult(keyBytes)
	return privKey
}

func createCertAndKeyBytes(key *ecdsa.PrivateKey) ([]byte, []byte, error) {
	// Create self-signed staking cert
	certTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(0),
		NotBefore:             time.Date(2000, time.January, 0, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Now().AddDate(100, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, key.Public(), key)
	if err != nil {
		return nil, nil, fmt.Errorf("couldn't create certificate: %w", err)
	}
	
	var certBuff bytes.Buffer
	if err := pem.Encode(&certBuff, &pem.Block{Type: "CERTIFICATE", Bytes: certBytes}); err != nil {
		return nil, nil, fmt.Errorf("couldn't write cert file: %w", err)
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("couldn't marshal private key: %w", err)
	}

	var keyBuff bytes.Buffer
	if err := pem.Encode(&keyBuff, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		return nil, nil, fmt.Errorf("couldn't write private key: %w", err)
	}
	
	return certBuff.Bytes(), keyBuff.Bytes(), nil
}
