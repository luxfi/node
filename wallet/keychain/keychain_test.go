// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keychain

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/luxfi/ids"
)

func TestCryptoKeychain_Secp256k1(t *testing.T) {
	require := require.New(t)
	
	// Create keychain with secp256k1 as default
	kc := NewKeychain(AlgoSecp256k1)
	
	// Generate a key
	addr, err := kc.GenerateKey()
	require.NoError(err)
	require.NotEqual(ids.ShortEmpty, addr)
	
	// Get the signer
	signer, exists := kc.Get(addr)
	require.True(exists)
	require.NotNil(signer)
	
	// Sign a message
	msg := []byte("test message")
	sig, err := signer.Sign(msg)
	require.NoError(err)
	require.NotEmpty(sig)
	
	// Check algorithm
	keySigner := signer.(*KeySigner)
	require.Equal(AlgoSecp256k1, keySigner.Algorithm())
}

func TestCryptoKeychain_MLDSA(t *testing.T) {
	require := require.New(t)
	
	testCases := []struct {
		name string
		algo Algorithm
	}{
		{"ML-DSA-44", AlgoMLDSA44},
		{"ML-DSA-65", AlgoMLDSA65},
		{"ML-DSA-87", AlgoMLDSA87},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			kc := NewKeychain(tc.algo)
			
			addr, err := kc.GenerateKey()
			require.NoError(err)
			require.NotEqual(ids.ShortEmpty, addr)
			
			signer, exists := kc.Get(addr)
			require.True(exists)
			
			msg := []byte("test message for ML-DSA")
			sig, err := signer.Sign(msg)
			require.NoError(err)
			require.NotEmpty(sig)
			
			keySigner := signer.(*KeySigner)
			require.Equal(tc.algo, keySigner.Algorithm())
		})
	}
}

func TestCryptoKeychain_SLHDSA(t *testing.T) {
	require := require.New(t)
	
	testCases := []struct {
		name string
		algo Algorithm
	}{
		{"SLH-DSA-128s", AlgoSLHDSA128s},
		{"SLH-DSA-192s", AlgoSLHDSA192s},
		{"SLH-DSA-256s", AlgoSLHDSA256s},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			kc := NewKeychain(tc.algo)
			
			addr, err := kc.GenerateKey()
			require.NoError(err)
			require.NotEqual(ids.ShortEmpty, addr)
			
			signer, exists := kc.Get(addr)
			require.True(exists)
			
			msg := []byte("test message for SLH-DSA")
			sig, err := signer.Sign(msg)
			require.NoError(err)
			require.NotEmpty(sig)
			
			keySigner := signer.(*KeySigner)
			require.Equal(tc.algo, keySigner.Algorithm())
		})
	}
}

func TestCryptoKeychain_MLKEM(t *testing.T) {
	require := require.New(t)
	
	testCases := []struct {
		name string
		algo Algorithm
	}{
		{"ML-KEM-512", AlgoMLKEM512},
		{"ML-KEM-768", AlgoMLKEM768},
		{"ML-KEM-1024", AlgoMLKEM1024},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			kc := NewKeychain(tc.algo)
			
			addr, err := kc.GenerateKey()
			require.NoError(err)
			require.NotEqual(ids.ShortEmpty, addr)
			
			signer, exists := kc.Get(addr)
			require.True(exists)
			
			// ML-KEM is for key encapsulation, not signing
			keySigner := signer.(*KeySigner)
			require.Equal(tc.algo, keySigner.Algorithm())
			
			// Test encapsulation
			ct, ss, err := keySigner.Encapsulate()
			require.NoError(err)
			require.NotEmpty(ct)
			require.NotEmpty(ss)
			
			// Test decapsulation
			ss2, err := keySigner.Decapsulate(ct)
			require.NoError(err)
			require.True(bytes.Equal(ss, ss2))
		})
	}
}

func TestCryptoKeychain_MultipleAlgorithms(t *testing.T) {
	require := require.New(t)
	
	kc := NewKeychain(AlgoSecp256k1)
	
	// Generate multiple keys with different algorithms
	addr1, err := kc.GenerateKeyWithAlgo(AlgoSecp256k1)
	require.NoError(err)
	
	addr2, err := kc.GenerateKeyWithAlgo(AlgoMLDSA44)
	require.NoError(err)
	
	addr3, err := kc.GenerateKeyWithAlgo(AlgoSLHDSA128s)
	require.NoError(err)
	
	// All addresses should be different
	require.NotEqual(addr1, addr2)
	require.NotEqual(addr2, addr3)
	require.NotEqual(addr1, addr3)
	
	// All signers should exist
	signer1, exists := kc.Get(addr1)
	require.True(exists)
	require.Equal(AlgoSecp256k1, signer1.(*KeySigner).Algorithm())
	
	signer2, exists := kc.Get(addr2)
	require.True(exists)
	require.Equal(AlgoMLDSA44, signer2.(*KeySigner).Algorithm())
	
	signer3, exists := kc.Get(addr3)
	require.True(exists)
	require.Equal(AlgoSLHDSA128s, signer3.(*KeySigner).Algorithm())
	
	// Check addresses set
	addrs := kc.Addresses()
	require.Equal(3, addrs.Len())
	require.True(addrs.Contains(addr1))
	require.True(addrs.Contains(addr2))
	require.True(addrs.Contains(addr3))
}

func TestCryptoKeychain_Compatibility(t *testing.T) {
	require := require.New(t)
	
	// Test that New() creates a secp256k1 keychain for backward compatibility
	kc := New()
	
	addr, err := kc.GenerateKey()
	require.NoError(err)
	
	signer, exists := kc.Get(addr)
	require.True(exists)
	
	keySigner := signer.(*KeySigner)
	require.Equal(AlgoSecp256k1, keySigner.Algorithm())
	
	// Test SignHash for secp256k1
	hash := make([]byte, 32)
	copy(hash, []byte("test hash 32 bytes long........!"))
	
	sig, err := signer.SignHash(hash)
	require.NoError(err)
	require.NotEmpty(sig)
}