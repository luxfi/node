// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"bytes"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/filesystem/perms"
)

var (
	ErrMLDSAVerificationFailure = errors.New("staking: ML-DSA verification failure")
	ErrInvalidPQKeySize         = errors.New("staking: invalid post-quantum key size")
)

// PQKeyPair represents a post-quantum ML-DSA key pair for hybrid staking
type PQKeyPair struct {
	PrivateKey *mldsa.PrivateKey
	PublicKey  *mldsa.PublicKey
	Mode       mldsa.Mode
}

// NewPQKeyPair generates a new ML-DSA-65 key pair (NIST Level 3, 192-bit security)
func NewPQKeyPair() (*PQKeyPair, error) {
	priv, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA65)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ML-DSA key: %w", err)
	}
	return &PQKeyPair{
		PrivateKey: priv,
		PublicKey:  priv.PublicKey,
		Mode:       mldsa.MLDSA65,
	}, nil
}

// Sign creates an ML-DSA signature over the given message
func (kp *PQKeyPair) Sign(msg []byte) ([]byte, error) {
	return kp.PrivateKey.Sign(rand.Reader, msg, nil)
}

// Verify checks an ML-DSA signature
func (kp *PQKeyPair) Verify(msg, sig []byte) bool {
	return kp.PublicKey.VerifySignature(msg, sig)
}

// PublicKeyBytes returns the serialized public key
func (kp *PQKeyPair) PublicKeyBytes() []byte {
	return kp.PublicKey.Bytes()
}

// PrivateKeyBytes returns the serialized private key
func (kp *PQKeyPair) PrivateKeyBytes() []byte {
	return kp.PrivateKey.Bytes()
}

// InitNodePQKeyPair generates and stores an ML-DSA key pair for hybrid staking.
// The keys will be placed at [keyPath] and [pubKeyPath].
// If there is already a file at [keyPath], returns nil (no-op).
func InitNodePQKeyPair(keyPath, pubKeyPath string) error {
	// If there is already a file at [keyPath], do nothing
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		return nil
	}

	kp, err := NewPQKeyPair()
	if err != nil {
		return err
	}

	// Ensure directories exist
	if err := os.MkdirAll(filepath.Dir(keyPath), perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("couldn't create path for PQ key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(pubKeyPath), perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("couldn't create path for PQ public key: %w", err)
	}

	// Write private key
	var keyBuff bytes.Buffer
	if err := pem.Encode(&keyBuff, &pem.Block{
		Type:  "ML-DSA-65 PRIVATE KEY",
		Bytes: kp.PrivateKeyBytes(),
	}); err != nil {
		return fmt.Errorf("couldn't encode PQ private key: %w", err)
	}
	if err := os.WriteFile(keyPath, keyBuff.Bytes(), perms.ReadOnly); err != nil {
		return fmt.Errorf("couldn't write PQ private key: %w", err)
	}

	// Write public key
	var pubBuff bytes.Buffer
	if err := pem.Encode(&pubBuff, &pem.Block{
		Type:  "ML-DSA-65 PUBLIC KEY",
		Bytes: kp.PublicKeyBytes(),
	}); err != nil {
		return fmt.Errorf("couldn't encode PQ public key: %w", err)
	}
	if err := os.WriteFile(pubKeyPath, pubBuff.Bytes(), perms.ReadOnly); err != nil {
		return fmt.Errorf("couldn't write PQ public key: %w", err)
	}

	return nil
}

// LoadPQKeyPair loads an ML-DSA key pair from PEM files
func LoadPQKeyPair(keyPath, pubKeyPath string) (*PQKeyPair, error) {
	// Load private key
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("couldn't read PQ private key: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "ML-DSA-65 PRIVATE KEY" {
		return nil, errors.New("failed to decode PQ private key PEM")
	}
	priv, err := mldsa.PrivateKeyFromBytes(mldsa.MLDSA65, keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("couldn't parse PQ private key: %w", err)
	}

	// Load public key
	pubPEM, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return nil, fmt.Errorf("couldn't read PQ public key: %w", err)
	}
	pubBlock, _ := pem.Decode(pubPEM)
	if pubBlock == nil || pubBlock.Type != "ML-DSA-65 PUBLIC KEY" {
		return nil, errors.New("failed to decode PQ public key PEM")
	}
	pub, err := mldsa.PublicKeyFromBytes(pubBlock.Bytes, mldsa.MLDSA65)
	if err != nil {
		return nil, fmt.Errorf("couldn't parse PQ public key: %w", err)
	}

	return &PQKeyPair{
		PrivateKey: priv,
		PublicKey:  pub,
		Mode:       mldsa.MLDSA65,
	}, nil
}

// VerifyPQSignature verifies an ML-DSA signature given a public key
func VerifyPQSignature(pubKeyBytes, msg, sig []byte) error {
	pub, err := mldsa.PublicKeyFromBytes(pubKeyBytes, mldsa.MLDSA65)
	if err != nil {
		return fmt.Errorf("invalid PQ public key: %w", err)
	}
	if !pub.VerifySignature(msg, sig) {
		return ErrMLDSAVerificationFailure
	}
	return nil
}
