// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package localsigner

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/node/utils/perms"
)

var (
	ErrFailedSecretKeyDeserialize            = errors.New("couldn't deserialize secret key")
	_                             bls.Signer = (*LocalSigner)(nil)
)

type LocalSigner struct {
	sk *bls.SecretKey
	pk *bls.PublicKey
}

// NewSecretKey generates a new secret key from the local source of
// cryptographically secure randomness.
func New() (*LocalSigner, error) {
	sk, err := bls.NewSecretKey()
	if err != nil {
		return nil, err
	}
	pk := sk.PublicKey()

	return &LocalSigner{sk: sk, pk: pk}, nil
}

// ToBytes returns the big-endian format of the secret key.
func (s *LocalSigner) ToBytes() []byte {
	return bls.SecretKeyToBytes(s.sk)
}

// FromBytes parses the big-endian format of the secret key into a
// secret key.
func FromBytes(skBytes []byte) (*LocalSigner, error) {
	sk, err := bls.SecretKeyFromBytes(skBytes)
	if err != nil {
		return nil, ErrFailedSecretKeyDeserialize
	}
	pk := sk.PublicKey()

	return &LocalSigner{sk: sk, pk: pk}, nil
}

func FromFile(keyPath string) (bls.Signer, error) {
	signingKeyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("could not read signing key from %s: %w", keyPath, err)
	}

	signer, err := FromBytes(signingKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("could not parse signing key: %w", err)
	}

	return signer, nil
}

func (s *LocalSigner) ToFile(keyPath string) error {
	if err := os.MkdirAll(filepath.Dir(keyPath), perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("could not create path for signing key at %s: %w", keyPath, err)
	}

	if err := os.WriteFile(
		keyPath,
		s.ToBytes(),
		perms.ReadWrite,
	); err != nil {
		return fmt.Errorf("could not write new signing key to %s: %w", keyPath, err)
	}

	if err := os.Chmod(keyPath, perms.ReadOnly); err != nil {
		return fmt.Errorf("could not restrict permissions on new signing key at %s: %w", keyPath, err)
	}

	return nil
}

func FromFileOrPersistNew(keyPath string) (bls.Signer, error) {
	_, err := os.Stat(keyPath)
	if !errors.Is(err, fs.ErrNotExist) {
		return FromFile(keyPath)
	}

	signer, err := New()
	if err != nil {
		return nil, fmt.Errorf("could not generate new signing key: %w", err)
	}

	if err := signer.ToFile(keyPath); err != nil {
		return nil, fmt.Errorf("could not persist new signer: %w", err)
	}

	return signer, nil
}

// PublicKey returns the public key that corresponds to this secret
// key.
func (s *LocalSigner) PublicKey() *bls.PublicKey {
	return s.pk
}

// Sign [msg] to authorize this message
func (s *LocalSigner) Sign(msg []byte) (*bls.Signature, error) {
	return s.sk.Sign(msg)
}

// Sign [msg] to prove the ownership
func (s *LocalSigner) SignProofOfPossession(msg []byte) (*bls.Signature, error) {
	return s.sk.SignProofOfPossession(msg)
}

func (*LocalSigner) Shutdown() error {
	return nil
}
