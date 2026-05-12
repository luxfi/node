// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keychain

import (
	"crypto"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/crypto/slhdsa"
	"github.com/luxfi/ids"
)

func SkipTestPQKeychain_Secp256k1(t *testing.T) {
	require := require.New(t)

	kc := NewPQKeychain(KeyTypeSecp256k1)

	// Generate a key
	addr, err := kc.GenerateKey()
	require.NoError(err)
	require.NotEqual(ids.ShortEmpty, addr)

	// Get the signer
	signer, exists := kc.Get(addr)
	require.True(exists)
	require.NotNil(signer)

	// Test signing
	msg := []byte("test message")
	sig, err := signer.Sign(msg)
	require.NoError(err)
	require.NotEmpty(sig)

	// Test sign hash
	hash := []byte("test hash 32 bytes long........!") // Exactly 32 bytes
	sigHash, err := signer.SignHash(hash)
	require.NoError(err)
	require.NotEmpty(sigHash)
}

func SkipTestPQKeychain_MLDSA44(t *testing.T) {
	require := require.New(t)

	kc := NewPQKeychain(KeyTypeMLDSA44)

	// Generate a key
	addr, err := kc.GenerateKey()
	require.NoError(err)
	require.NotEqual(ids.ShortEmpty, addr)

	// Get the signer
	signer, exists := kc.Get(addr)
	require.True(exists)
	require.NotNil(signer)

	// Test signing
	msg := []byte("test message for ML-DSA-44")
	sig, err := signer.Sign(msg)
	require.NoError(err)
	require.NotEmpty(sig)

	// Verify signature has expected size for ML-DSA-44
	// ML-DSA-44 signature is 2420 bytes
	require.Equal(2420, len(sig))
}

func SkipTestPQKeychain_MLDSA65(t *testing.T) {
	require := require.New(t)

	kc := NewPQKeychain(KeyTypeMLDSA65)

	// Generate a key
	addr, err := kc.GenerateKey()
	require.NoError(err)
	require.NotEqual(ids.ShortEmpty, addr)

	// Get the signer
	signer, exists := kc.Get(addr)
	require.True(exists)
	require.NotNil(signer)

	// Test signing
	msg := []byte("test message for ML-DSA-65")
	sig, err := signer.Sign(msg)
	require.NoError(err)
	require.NotEmpty(sig)

	// ML-DSA-65 signature is 3293 bytes
	require.Equal(3293, len(sig))
}

func SkipTestPQKeychain_MLDSA87(t *testing.T) {
	require := require.New(t)

	kc := NewPQKeychain(KeyTypeMLDSA87)

	// Generate a key
	addr, err := kc.GenerateKey()
	require.NoError(err)
	require.NotEqual(ids.ShortEmpty, addr)

	// Get the signer
	signer, exists := kc.Get(addr)
	require.True(exists)
	require.NotNil(signer)

	// Test signing
	msg := []byte("test message for ML-DSA-87")
	sig, err := signer.Sign(msg)
	require.NoError(err)
	require.NotEmpty(sig)

	// ML-DSA-87 signature is 4595 bytes
	require.Equal(4595, len(sig))
}

func SkipTestPQKeychain_SLHDSA128(t *testing.T) {
	require := require.New(t)

	kc := NewPQKeychain(KeyTypeSLHDSA128)

	// Generate a key
	addr, err := kc.GenerateKey()
	require.NoError(err)
	require.NotEqual(ids.ShortEmpty, addr)

	// Get the signer
	signer, exists := kc.Get(addr)
	require.True(exists)
	require.NotNil(signer)

	// Test signing
	msg := []byte("test message for SLH-DSA-128")
	sig, err := signer.Sign(msg)
	require.NoError(err)
	require.NotEmpty(sig)

	// SLH-DSA-128s signature is 7856 bytes
	require.Equal(7856, len(sig))
}

func SkipTestPQKeychain_Hybrid(t *testing.T) {
	require := require.New(t)

	kc := NewPQKeychain(KeyTypeHybridSecp256k1MLDSA44)

	// Generate a hybrid key
	addr, err := kc.GenerateKey()
	require.NoError(err)
	require.NotEqual(ids.ShortEmpty, addr)

	// Get the signer
	signer, exists := kc.Get(addr)
	require.True(exists)
	require.NotNil(signer)

	// Test signing
	msg := []byte("test message for hybrid signature")
	sig, err := signer.Sign(msg)
	require.NoError(err)
	require.NotEmpty(sig)

	// Hybrid signature should have both components
	// Format: [2 bytes classical len][classical sig][2 bytes PQ len][PQ sig]
	require.Greater(len(sig), 2420) // At least ML-DSA-44 size

	// Parse the hybrid signature
	classicalLen := int(sig[0])<<8 | int(sig[1])
	require.Greater(classicalLen, 0)
	require.Less(classicalLen, 100) // secp256k1 sig is ~65 bytes

	pqOffset := 2 + classicalLen
	pqLen := int(sig[pqOffset])<<8 | int(sig[pqOffset+1])
	require.Equal(2420, pqLen) // ML-DSA-44 signature size
}

func SkipTestPQKeychain_MultipleKeys(t *testing.T) {
	require := require.New(t)

	kc := NewPQKeychain(KeyTypeSecp256k1)

	// Add multiple keys of different types
	secp256k1Key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	addr1 := kc.AddSecp256k1(secp256k1Key)

	mldsaKey, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA44)
	require.NoError(err)
	addr2 := kc.AddMLDSA(mldsaKey, KeyTypeMLDSA44)

	slhdsaKey, err := slhdsa.GenerateKey(rand.Reader, slhdsa.SHA2_128s)
	require.NoError(err)
	addr3 := kc.AddSLHDSA(slhdsaKey, KeyTypeSLHDSA128)

	// Check all addresses are present
	addrs := kc.Addresses()
	require.Len(addrs, 3)

	// Verify each key can sign
	for _, addr := range []ids.ShortID{addr1, addr2, addr3} {
		signer, exists := kc.Get(addr)
		require.True(exists)

		msg := []byte("test message")
		sig, err := signer.Sign(msg)
		require.NoError(err)
		require.NotEmpty(sig)
	}
}

func SkipTestPQKeychain_AddressUniqueness(t *testing.T) {
	require := require.New(t)

	kc := NewPQKeychain(KeyTypeMLDSA44)

	// Generate multiple keys and ensure addresses are unique
	addresses := make(map[ids.ShortID]bool)

	for i := 0; i < 10; i++ {
		addr, err := kc.GenerateKey()
		require.NoError(err)
		require.NotEqual(ids.ShortEmpty, addr)

		// Check address is unique
		require.False(addresses[addr], "duplicate address generated")
		addresses[addr] = true
	}

	// Verify keychain has all addresses
	require.Len(kc.Addresses(), 10)
}

func SkipTestPQKeychain_SignatureVerification(t *testing.T) {
	require := require.New(t)

	// Test ML-DSA-44 signature verification
	privKey, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA44)
	require.NoError(err)

	kc := NewPQKeychain(KeyTypeMLDSA44)
	addr := kc.AddMLDSA(privKey, KeyTypeMLDSA44)

	signer, exists := kc.Get(addr)
	require.True(exists)

	// Sign a message
	msg := []byte("test message for verification")
	sig, err := signer.Sign(msg)
	require.NoError(err)

	// Verify the signature using the public key
	pubKey := privKey.PublicKey
	valid := pubKey.Verify(msg, sig, crypto.Hash(0))
	require.True(valid, "signature verification failed")

	// Test with wrong message
	wrongMsg := []byte("wrong message")
	valid = pubKey.Verify(wrongMsg, sig, crypto.Hash(0))
	require.False(valid, "signature should not verify with wrong message")
}

func BenchmarkPQKeychain_Secp256k1_Sign(b *testing.B) {
	kc := NewPQKeychain(KeyTypeSecp256k1)
	addr, _ := kc.GenerateKey()
	signer, _ := kc.Get(addr)
	msg := []byte("benchmark message")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = signer.Sign(msg)
	}
}

func BenchmarkPQKeychain_MLDSA44_Sign(b *testing.B) {
	kc := NewPQKeychain(KeyTypeMLDSA44)
	addr, _ := kc.GenerateKey()
	signer, _ := kc.Get(addr)
	msg := []byte("benchmark message")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = signer.Sign(msg)
	}
}

func BenchmarkPQKeychain_SLHDSA128_Sign(b *testing.B) {
	kc := NewPQKeychain(KeyTypeSLHDSA128)
	addr, _ := kc.GenerateKey()
	signer, _ := kc.Get(addr)
	msg := []byte("benchmark message")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = signer.Sign(msg)
	}
}

func BenchmarkPQKeychain_Hybrid_Sign(b *testing.B) {
	kc := NewPQKeychain(KeyTypeHybridSecp256k1MLDSA44)
	addr, _ := kc.GenerateKey()
	signer, _ := kc.Get(addr)
	msg := []byte("benchmark message")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = signer.Sign(msg)
	}
}

// TestPQSigner_TypeSafety ensures type safety for different key types
func TestPQSigner_TypeSafety(t *testing.T) {
	require := require.New(t)

	kc := NewPQKeychain(KeyTypeSecp256k1)

	// Add a secp256k1 key
	secp256k1Key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	addr := kc.AddSecp256k1(secp256k1Key)

	// Get as PQSigner to access key type
	pqSigner, exists := kc.GetPQSigner(addr)
	require.True(exists)
	require.Equal(KeyTypeSecp256k1, pqSigner.keyType)

	// Add an ML-DSA key
	mldsaKey, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA44)
	require.NoError(err)
	addr2 := kc.AddMLDSA(mldsaKey, KeyTypeMLDSA44)

	pqSigner2, exists := kc.GetPQSigner(addr2)
	require.True(exists)
	require.Equal(KeyTypeMLDSA44, pqSigner2.keyType)

	// Ensure they have different addresses
	require.NotEqual(addr, addr2)
}

// TestPQKeychain_RingSigSupport tests ring-signature (LSAG) support
func TestPQKeychain_RingSigSupport(t *testing.T) {
	require := require.New(t)

	// Test that the PQ keychain is ready for future corona integration
	kc := NewPQKeychain(KeyTypeSecp256k1)
	require.NotNil(kc)

	// Verify that current keychain supports existing key types
	// This lays groundwork for future ring-signature (LSAG) support
	supportedTypes := []KeyType{
		KeyTypeSecp256k1,
		KeyTypeMLDSA44,
		KeyTypeMLDSA65,
		KeyTypeMLDSA87,
		KeyTypeSLHDSA128,
		KeyTypeSLHDSA192,
		KeyTypeSLHDSA256,
	}

	// Test that keychain can be created with supported key types
	for _, keyType := range supportedTypes {
		testKc := NewPQKeychain(keyType)
		require.NotNil(testKc, "Should be able to create keychain with key type %v", keyType)
	}

	// Test that corona key type is defined (ready for future implementation)
	ringSigKc := NewPQKeychain(KeyTypeRingSig)
	require.NotNil(ringSigKc, "Should be able to create keychain with KeyTypeRingSig")

	// Future corona ring signature implementation would add:
	// - Ring signature generation
	// - Ring signature verification
	// - Key ring management
}

// TestPQKeychain_BLS tests BLS key support
func TestPQKeychain_BLS(t *testing.T) {
	require := require.New(t)

	kc := NewPQKeychain(KeyTypeBLS)

	// Generate a BLS key
	addr, err := kc.GenerateKey()
	require.NoError(err)
	require.NotEqual(ids.ShortEmpty, addr)

	// Get the signer
	signer, exists := kc.Get(addr)
	require.True(exists)
	require.NotNil(signer)

	// Test signing
	msg := []byte("test message for BLS signature")
	sig, err := signer.Sign(msg)
	require.NoError(err)
	require.NotEmpty(sig)

	// BLS signature is 96 bytes (G2 point)
	require.Equal(96, len(sig), "BLS signature should be 96 bytes")

	// Get the PQ signer for advanced operations
	pqSigner, exists := kc.GetPQSigner(addr)
	require.True(exists)
	require.Equal(KeyTypeBLS, pqSigner.KeyType())

	// Test public key retrieval
	pubKey := pqSigner.PublicKey()
	require.NotEmpty(pubKey)
	require.Equal(48, len(pubKey), "BLS public key should be 48 bytes")

	// Test BLS public key method
	blsPubKey := pqSigner.BLSPublicKey()
	require.NotNil(blsPubKey)
}

// TestPQKeychain_MLKEM tests ML-KEM key encapsulation support
func TestPQKeychain_MLKEM(t *testing.T) {
	require := require.New(t)

	// Test all ML-KEM security levels
	testCases := []struct {
		keyType KeyType
		name    string
	}{
		{KeyTypeMLKEM512, "ML-KEM-512"},
		{KeyTypeMLKEM768, "ML-KEM-768"},
		{KeyTypeMLKEM1024, "ML-KEM-1024"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			kc := NewPQKeychain(tc.keyType)

			// Generate ML-KEM key pair
			addr, err := kc.GenerateKey()
			require.NoError(err)
			require.NotEqual(ids.ShortEmpty, addr)

			// Get the PQ signer
			pqSigner, exists := kc.GetPQSigner(addr)
			require.True(exists)
			require.Equal(tc.keyType, pqSigner.KeyType())

			// Test public key retrieval
			pubKey := pqSigner.PublicKey()
			require.NotEmpty(pubKey)
		})
	}
}

// TestPQKeychain_RingSig tests ring signature functionality
func TestPQKeychain_RingSig(t *testing.T) {
	require := require.New(t)

	kc := NewPQKeychain(KeyTypeRingSig)

	// Generate a Corona key (defaults to LSAG)
	addr, err := kc.GenerateKey()
	require.NoError(err)
	require.NotEqual(ids.ShortEmpty, addr)

	// Get the PQ signer
	pqSigner, exists := kc.GetPQSigner(addr)
	require.True(exists)
	require.Equal(KeyTypeRingSig, pqSigner.KeyType())

	// Get public key for the ring
	signerPubKey := pqSigner.PublicKey()
	require.NotEmpty(signerPubKey)

	// Generate additional public keys for the ring
	decoySigner1, err := kc.GenerateKey()
	require.NoError(err)
	decoy1, _ := kc.GetPQSigner(decoySigner1)

	decoySigner2, err := kc.GenerateKey()
	require.NoError(err)
	decoy2, _ := kc.GetPQSigner(decoySigner2)

	// Create the ring
	ringPubKeys := [][]byte{
		decoy1.PublicKey(),
		signerPubKey,
		decoy2.PublicKey(),
	}
	signerIndex := 1 // Our signer is at index 1

	// Create ring signature
	message := []byte("private transaction data")
	ringSig, err := pqSigner.SignRing(message, ringPubKeys, signerIndex)
	require.NoError(err)
	require.NotNil(ringSig)

	// Verify the signature
	valid := ringSig.Verify(message, ringPubKeys)
	require.True(valid, "Ring signature should verify")

	// Test key image (for linkability)
	keyImage := pqSigner.KeyImage()
	require.NotEmpty(keyImage)

	// Verify wrong message fails
	wrongMsg := []byte("wrong message")
	valid = ringSig.Verify(wrongMsg, ringPubKeys)
	require.False(valid, "Ring signature should not verify with wrong message")
}

// TestPQKeychain_RingSigLattice tests post-quantum lattice ring signatures
func TestPQKeychain_RingSigLattice(t *testing.T) {
	require := require.New(t)

	kc := NewPQKeychain(KeyTypeSecp256k1) // Use any type, we'll use GenerateCoronaKey

	// Generate a lattice-based ring signature key
	// LatticeLSAG = 1 in the ring.Scheme enum
	addr, err := kc.GenerateCoronaKey(1) // 1 = LatticeLSAG scheme (ML-DSA based)
	require.NoError(err)
	require.NotEqual(ids.ShortEmpty, addr)

	pqSigner, exists := kc.GetPQSigner(addr)
	require.True(exists)
	require.Equal(KeyTypeRingSig, pqSigner.KeyType())

	// Verify the scheme is LatticeLSAG (value 1)
	require.Equal(1, int(pqSigner.RingScheme()))
}

// TestPQKeychain_HybridBLSMLDSA tests hybrid BLS + ML-DSA signatures
func TestPQKeychain_HybridBLSMLDSA(t *testing.T) {
	require := require.New(t)

	kc := NewPQKeychain(KeyTypeHybridBLSMLDSA44)

	// Generate hybrid key
	addr, err := kc.GenerateKey()
	require.NoError(err)
	require.NotEqual(ids.ShortEmpty, addr)

	// Get the signer
	signer, exists := kc.Get(addr)
	require.True(exists)
	require.NotNil(signer)

	// Test signing
	msg := []byte("test message for hybrid BLS+ML-DSA signature")
	sig, err := signer.Sign(msg)
	require.NoError(err)
	require.NotEmpty(sig)

	// Hybrid signature should have both components
	// Format: [2 bytes BLS len][BLS sig][2 bytes PQ len][PQ sig]
	require.Greater(len(sig), 96+2420) // BLS (96) + ML-DSA-44 (2420) + overhead

	// Parse the hybrid signature
	blsLen := int(sig[0])<<8 | int(sig[1])
	require.Equal(96, blsLen, "BLS signature should be 96 bytes")

	pqOffset := 2 + blsLen
	pqLen := int(sig[pqOffset])<<8 | int(sig[pqOffset+1])
	require.Equal(2420, pqLen, "ML-DSA-44 signature should be 2420 bytes")

	// Get the PQ signer for advanced operations
	pqSigner, exists := kc.GetPQSigner(addr)
	require.True(exists)
	require.Equal(KeyTypeHybridBLSMLDSA44, pqSigner.KeyType())

	// Test BLS public key retrieval for hybrid
	blsPubKey := pqSigner.BLSPublicKey()
	require.NotNil(blsPubKey)
}

// TestPQKeychain_AllKeyTypes tests that all key types can be generated
func TestPQKeychain_AllKeyTypes(t *testing.T) {
	require := require.New(t)

	keyTypes := []KeyType{
		KeyTypeSecp256k1,
		KeyTypeBLS,
		KeyTypeMLDSA44,
		KeyTypeMLDSA65,
		KeyTypeMLDSA87,
		KeyTypeSLHDSA128,
		KeyTypeSLHDSA192,
		KeyTypeSLHDSA256,
		KeyTypeMLKEM512,
		KeyTypeMLKEM768,
		KeyTypeMLKEM1024,
		KeyTypeRingSig,
		KeyTypeHybridSecp256k1MLDSA44,
		KeyTypeHybridSecp256k1SLHDSA128,
		KeyTypeHybridBLSMLDSA44,
	}

	for _, keyType := range keyTypes {
		t.Run(keyTypeName(keyType), func(t *testing.T) {
			kc := NewPQKeychain(keyType)
			addr, err := kc.GenerateKey()
			require.NoError(err, "Should be able to generate key type %v", keyType)
			require.NotEqual(ids.ShortEmpty, addr)
		})
	}
}

func keyTypeName(kt KeyType) string {
	names := map[KeyType]string{
		KeyTypeSecp256k1:              "Secp256k1",
		KeyTypeBLS:                    "BLS",
		KeyTypeMLDSA44:                "MLDSA44",
		KeyTypeMLDSA65:                "MLDSA65",
		KeyTypeMLDSA87:                "MLDSA87",
		KeyTypeSLHDSA128:              "SLHDSA128",
		KeyTypeSLHDSA192:              "SLHDSA192",
		KeyTypeSLHDSA256:              "SLHDSA256",
		KeyTypeMLKEM512:               "MLKEM512",
		KeyTypeMLKEM768:               "MLKEM768",
		KeyTypeMLKEM1024:              "MLKEM1024",
		KeyTypeRingSig:               "Corona",
		KeyTypeHybridSecp256k1MLDSA44: "HybridSecp256k1MLDSA44",
		KeyTypeHybridSecp256k1SLHDSA128: "HybridSecp256k1SLHDSA128",
		KeyTypeHybridBLSMLDSA44:       "HybridBLSMLDSA44",
	}
	if name, ok := names[kt]; ok {
		return name
	}
	return "Unknown"
}

// BenchmarkPQKeychain_BLS_Sign benchmarks BLS signing
func BenchmarkPQKeychain_BLS_Sign(b *testing.B) {
	kc := NewPQKeychain(KeyTypeBLS)
	addr, _ := kc.GenerateKey()
	signer, _ := kc.Get(addr)
	msg := []byte("benchmark message")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = signer.Sign(msg)
	}
}

// BenchmarkPQKeychain_HybridBLSMLDSA_Sign benchmarks hybrid BLS+ML-DSA signing
func BenchmarkPQKeychain_HybridBLSMLDSA_Sign(b *testing.B) {
	kc := NewPQKeychain(KeyTypeHybridBLSMLDSA44)
	addr, _ := kc.GenerateKey()
	signer, _ := kc.Get(addr)
	msg := []byte("benchmark message")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = signer.Sign(msg)
	}
}
