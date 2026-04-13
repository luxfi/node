// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dialer

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/net/endpoints"
)

func TestNewRNSIdentity(t *testing.T) {
	id, err := NewRNSIdentity()
	require.NoError(t, err)
	require.NotNil(t, id)

	// Verify key lengths
	require.Len(t, id.PublicKey(), ed25519PublicKeySize)
	require.Len(t, id.EncryptionPublicKey(), x25519KeySize)

	// Destination should be non-zero
	dest := id.Destination()
	require.Len(t, dest, endpoints.RNSDestinationLen)
	require.False(t, isZero(dest[:]))
}

func TestRNSIdentity_Deterministic(t *testing.T) {
	// Same seed should produce identical identity
	seed := make([]byte, ed25519SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}

	id1, err := newRNSIdentityFromSeed(seed)
	require.NoError(t, err)

	id2, err := newRNSIdentityFromSeed(seed)
	require.NoError(t, err)

	require.Equal(t, id1.Destination(), id2.Destination())
	require.Equal(t, id1.PublicKey(), id2.PublicKey())
	require.Equal(t, id1.EncryptionPublicKey(), id2.EncryptionPublicKey())
}

func TestRNSIdentity_SignVerify(t *testing.T) {
	id, err := NewRNSIdentity()
	require.NoError(t, err)

	message := []byte("test message for signing")
	signature := id.Sign(message)

	// Signature should be correct size
	require.Len(t, signature, ed25519SignatureSize)

	// Verify should succeed
	require.True(t, id.Verify(message, signature))

	// Verify with wrong message should fail
	require.False(t, id.Verify([]byte("wrong message"), signature))

	// Verify with tampered signature should fail
	tamperedSig := make([]byte, len(signature))
	copy(tamperedSig, signature)
	tamperedSig[0] ^= 0xFF
	require.False(t, id.Verify(message, tamperedSig))

	// Verify with wrong length signature should fail
	require.False(t, id.Verify(message, signature[:32]))
}

func TestVerifyWithPublicKey(t *testing.T) {
	id, err := NewRNSIdentity()
	require.NoError(t, err)

	message := []byte("test message")
	signature := id.Sign(message)

	// Verify with extracted public key
	require.True(t, VerifyWithPublicKey(id.PublicKey(), message, signature))

	// Wrong public key should fail
	id2, err := NewRNSIdentity()
	require.NoError(t, err)
	require.False(t, VerifyWithPublicKey(id2.PublicKey(), message, signature))

	// Wrong length keys should fail
	require.False(t, VerifyWithPublicKey([]byte{1, 2, 3}, message, signature))
	require.False(t, VerifyWithPublicKey(id.PublicKey(), message, []byte{1, 2, 3}))
}

func TestRNSIdentity_EncryptDecrypt(t *testing.T) {
	// Create sender and recipient identities
	sender, err := NewRNSIdentity()
	require.NoError(t, err)

	recipient, err := NewRNSIdentity()
	require.NoError(t, err)

	// Sender encrypts to recipient
	ephemeralPub, senderSecret, err := sender.Encrypt(recipient.EncryptionPublicKey())
	require.NoError(t, err)
	require.Len(t, ephemeralPub, x25519KeySize)
	require.Len(t, senderSecret, 32) // SHA-256 output

	// Recipient decrypts
	recipientSecret, err := recipient.Decrypt(ephemeralPub)
	require.NoError(t, err)

	// Shared secrets must match
	require.Equal(t, senderSecret, recipientSecret)
}

func TestRNSIdentity_EncryptDecrypt_DifferentEachTime(t *testing.T) {
	sender, err := NewRNSIdentity()
	require.NoError(t, err)

	recipient, err := NewRNSIdentity()
	require.NoError(t, err)

	// Two encryptions should produce different ephemeral keys
	ephPub1, secret1, err := sender.Encrypt(recipient.EncryptionPublicKey())
	require.NoError(t, err)

	ephPub2, secret2, err := sender.Encrypt(recipient.EncryptionPublicKey())
	require.NoError(t, err)

	require.NotEqual(t, ephPub1, ephPub2)
	require.NotEqual(t, secret1, secret2)

	// But both should decrypt correctly
	decrypted1, err := recipient.Decrypt(ephPub1)
	require.NoError(t, err)
	require.Equal(t, secret1, decrypted1)

	decrypted2, err := recipient.Decrypt(ephPub2)
	require.NoError(t, err)
	require.Equal(t, secret2, decrypted2)
}

func TestRNSIdentity_EncryptDecrypt_InvalidKey(t *testing.T) {
	id, err := NewRNSIdentity()
	require.NoError(t, err)

	// Wrong size recipient key
	_, _, err = id.Encrypt([]byte{1, 2, 3})
	require.ErrorIs(t, err, ErrInvalidIdentity)

	// Wrong size ephemeral key
	_, err = id.Decrypt([]byte{1, 2, 3})
	require.ErrorIs(t, err, ErrInvalidIdentity)
}

func TestRNSIdentity_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.rns")

	// Create and save identity
	original, err := NewRNSIdentity()
	require.NoError(t, err)

	err = original.Save(path)
	require.NoError(t, err)

	// Verify file permissions (unix only - Windows doesn't have Unix-style permissions)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	}

	// Load identity
	loaded, err := LoadRNSIdentity(path)
	require.NoError(t, err)

	// Verify all fields match
	require.Equal(t, original.Destination(), loaded.Destination())
	require.Equal(t, original.PublicKey(), loaded.PublicKey())
	require.Equal(t, original.EncryptionPublicKey(), loaded.EncryptionPublicKey())

	// Verify signing still works
	message := []byte("test after load")
	sig := loaded.Sign(message)
	require.True(t, original.Verify(message, sig))

	// Verify encryption still works
	ephPub, secret1, err := original.Encrypt(loaded.EncryptionPublicKey())
	require.NoError(t, err)
	secret2, err := loaded.Decrypt(ephPub)
	require.NoError(t, err)
	require.Equal(t, secret1, secret2)
}

func TestLoadRNSIdentity_InvalidFile(t *testing.T) {
	dir := t.TempDir()

	// Non-existent file
	_, err := LoadRNSIdentity(filepath.Join(dir, "nonexistent"))
	require.Error(t, err)

	// Wrong file size
	wrongSize := filepath.Join(dir, "wrongsize")
	require.NoError(t, os.WriteFile(wrongSize, []byte{1, 2, 3}, 0600))
	_, err = LoadRNSIdentity(wrongSize)
	require.ErrorIs(t, err, ErrInvalidIdentity)

	// Wrong magic number
	wrongMagic := filepath.Join(dir, "wrongmagic")
	data := make([]byte, rnsIdentityFileSize)
	require.NoError(t, os.WriteFile(wrongMagic, data, 0600))
	_, err = LoadRNSIdentity(wrongMagic)
	require.ErrorIs(t, err, ErrInvalidIdentity)

	// Wrong version
	wrongVersion := filepath.Join(dir, "wrongversion")
	data[0] = 'R'
	data[1] = 'N'
	data[2] = 'S'
	data[3] = 'I'
	data[7] = 99 // Invalid version
	require.NoError(t, os.WriteFile(wrongVersion, data, 0600))
	_, err = LoadRNSIdentity(wrongVersion)
	require.ErrorIs(t, err, ErrInvalidIdentity)
}

func TestPublicIdentity(t *testing.T) {
	// Create full identity
	full, err := NewRNSIdentity()
	require.NoError(t, err)

	// Create public identity from public keys
	pub, err := NewPublicIdentity(full.PublicKey(), full.EncryptionPublicKey())
	require.NoError(t, err)

	// Destinations should match
	require.Equal(t, full.Destination(), pub.Destination())

	// Verify signature with public identity
	message := []byte("signed by full identity")
	sig := full.Sign(message)
	require.True(t, pub.Verify(message, sig))

	// Marshal/unmarshal public identity
	data, err := pub.MarshalBinary()
	require.NoError(t, err)
	require.Len(t, data, ed25519PublicKeySize+x25519KeySize)

	restored, err := UnmarshalPublicIdentity(data)
	require.NoError(t, err)
	require.Equal(t, pub.Destination(), restored.Destination())
	require.Equal(t, pub.PublicKey(), restored.PublicKey())
	require.Equal(t, pub.EncryptionPublicKey(), restored.EncryptionPublicKey())
}

func TestPublicIdentity_InvalidKeys(t *testing.T) {
	// Wrong Ed25519 key size
	_, err := NewPublicIdentity([]byte{1, 2, 3}, make([]byte, x25519KeySize))
	require.ErrorIs(t, err, ErrInvalidIdentity)

	// Wrong X25519 key size
	_, err = NewPublicIdentity(make([]byte, ed25519PublicKeySize), []byte{1, 2, 3})
	require.ErrorIs(t, err, ErrInvalidIdentity)

	// Wrong unmarshal size
	_, err = UnmarshalPublicIdentity([]byte{1, 2, 3})
	require.ErrorIs(t, err, ErrInvalidIdentity)
}

func TestDestinationFromPublicKeys(t *testing.T) {
	id, err := NewRNSIdentity()
	require.NoError(t, err)

	dest, err := DestinationFromPublicKeys(id.PublicKey(), id.EncryptionPublicKey())
	require.NoError(t, err)
	require.Equal(t, id.Destination(), dest)

	// Invalid key sizes
	_, err = DestinationFromPublicKeys([]byte{1, 2, 3}, id.EncryptionPublicKey())
	require.ErrorIs(t, err, ErrInvalidIdentity)

	_, err = DestinationFromPublicKeys(id.PublicKey(), []byte{1, 2, 3})
	require.ErrorIs(t, err, ErrInvalidIdentity)
}

func TestRNSIdentity_Close(t *testing.T) {
	id, err := NewRNSIdentity()
	require.NoError(t, err)

	// Capture keys before close
	edPrivLen := len(id.edPrivateKey)
	xPrivLen := len(id.xPrivateKey)

	err = id.Close()
	require.NoError(t, err)

	// Private keys should be zeroed
	for i := 0; i < edPrivLen; i++ {
		require.Zero(t, id.edPrivateKey[i])
	}
	for i := 0; i < xPrivLen; i++ {
		require.Zero(t, id.xPrivateKey[i])
	}
}

func TestRNSIdentity_UniqueDestinations(t *testing.T) {
	// Generate multiple identities and verify unique destinations
	destinations := make(map[[endpoints.RNSDestinationLen]byte]bool)

	for i := 0; i < 100; i++ {
		id, err := NewRNSIdentity()
		require.NoError(t, err)

		dest := id.Destination()
		require.False(t, destinations[dest], "duplicate destination found")
		destinations[dest] = true
	}
}

func TestIsZero(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected bool
	}{
		{"all zeros", make([]byte, 32), true},
		{"first byte non-zero", []byte{1, 0, 0, 0}, false},
		{"last byte non-zero", []byte{0, 0, 0, 1}, false},
		{"middle byte non-zero", []byte{0, 1, 0, 0}, false},
		{"all non-zero", []byte{1, 2, 3, 4}, false},
		{"empty slice", []byte{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, isZero(tt.input))
		})
	}
}

func BenchmarkNewRNSIdentity(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := NewRNSIdentity()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRNSIdentity_Sign(b *testing.B) {
	id, _ := NewRNSIdentity()
	message := bytes.Repeat([]byte("x"), 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = id.Sign(message)
	}
}

func BenchmarkRNSIdentity_Verify(b *testing.B) {
	id, _ := NewRNSIdentity()
	message := bytes.Repeat([]byte("x"), 1024)
	sig := id.Sign(message)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = id.Verify(message, sig)
	}
}

func BenchmarkRNSIdentity_Encrypt(b *testing.B) {
	sender, _ := NewRNSIdentity()
	recipient, _ := NewRNSIdentity()
	recipientPub := recipient.EncryptionPublicKey()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = sender.Encrypt(recipientPub)
	}
}

func BenchmarkRNSIdentity_Decrypt(b *testing.B) {
	sender, _ := NewRNSIdentity()
	recipient, _ := NewRNSIdentity()
	ephPub, _, _ := sender.Encrypt(recipient.EncryptionPublicKey())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = recipient.Decrypt(ephPub)
	}
}
