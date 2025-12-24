// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/math/set"
	"github.com/stretchr/testify/require"
)

// TestHybridBLSRTSignatureNumSigners tests the NumSigners method
func TestHybridBLSRTSignatureNumSigners(t *testing.T) {
	require := require.New(t)

	tests := []struct {
		name           string
		signers        []byte
		expectedNum    int
		expectedErrMsg string
	}{
		{
			name:        "empty signers",
			signers:     []byte{},
			expectedNum: 0,
		},
		{
			name:        "single signer",
			signers:     []byte{0x01},
			expectedNum: 1,
		},
		{
			name:        "three signers",
			signers:     []byte{0x07}, // bits 0, 1, 2 set
			expectedNum: 3,
		},
		{
			name:        "eight signers",
			signers:     []byte{0xFF},
			expectedNum: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig := &HybridBLSRTSignature{
				Signers: tt.signers,
			}

			num, err := sig.NumSigners()
			if tt.expectedErrMsg != "" {
				require.ErrorContains(err, tt.expectedErrMsg)
			} else {
				require.NoError(err)
				require.Equal(tt.expectedNum, num)
			}
		})
	}
}

// TestHybridBLSRTSignatureString tests the String method
func TestHybridBLSRTSignatureString(t *testing.T) {
	require := require.New(t)

	sig := &HybridBLSRTSignature{
		Signers:           []byte{0x01},
		BLSSignature:      [bls.SignatureLen]byte{0x01, 0x02, 0x03},
		RingtailSignature: []byte{0x04, 0x05, 0x06},
	}

	str := sig.String()
	require.Contains(str, "HybridBLSRTSignature")
	require.Contains(str, "Signers")
	require.Contains(str, "BLS")
	require.Contains(str, "RT")
}

// TestAggregateRingtailPublicKeys tests Ringtail public key aggregation
func TestAggregateRingtailPublicKeys(t *testing.T) {
	require := require.New(t)

	tests := []struct {
		name           string
		publicKeys     [][]byte
		expectedErrMsg string
	}{
		{
			name:           "empty keys",
			publicKeys:     [][]byte{},
			expectedErrMsg: "no public keys",
		},
		{
			name: "single key",
			publicKeys: [][]byte{
				make([]byte, 32),
			},
		},
		{
			name: "two keys same length",
			publicKeys: [][]byte{
				make([]byte, 32),
				make([]byte, 32),
			},
		},
		{
			name: "inconsistent key lengths",
			publicKeys: [][]byte{
				make([]byte, 32),
				make([]byte, 64),
			},
			expectedErrMsg: "inconsistent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := AggregateRingtailPublicKeys(tt.publicKeys)
			if tt.expectedErrMsg != "" {
				require.ErrorContains(err, tt.expectedErrMsg)
				require.Nil(result)
			} else {
				require.NoError(err)
				require.NotNil(result)
				if len(tt.publicKeys) > 0 {
					require.Equal(len(tt.publicKeys[0]), len(result))
				}
			}
		})
	}
}

// TestVerifyRingtailSignature tests Ringtail signature verification
func TestVerifyRingtailSignature(t *testing.T) {
	require := require.New(t)

	tests := []struct {
		name      string
		publicKey []byte
		message   []byte
		signature []byte
		expected  bool
	}{
		{
			name:      "empty public key",
			publicKey: []byte{},
			message:   []byte("test message"),
			signature: make([]byte, 64),
			expected:  false,
		},
		{
			name:      "short public key",
			publicKey: make([]byte, 16),
			message:   []byte("test message"),
			signature: make([]byte, 64),
			expected:  false,
		},
		{
			name:      "empty signature",
			publicKey: make([]byte, 32),
			message:   []byte("test message"),
			signature: []byte{},
			expected:  false,
		},
		{
			name:      "short signature",
			publicKey: make([]byte, 32),
			message:   []byte("test message"),
			signature: make([]byte, 32),
			expected:  false,
		},
		{
			name:      "valid structure",
			publicKey: make([]byte, 32),
			message:   []byte("test message"),
			signature: make([]byte, 64),
			expected:  true, // Placeholder verification accepts valid structure
		},
		{
			name:      "empty message",
			publicKey: make([]byte, 32),
			message:   []byte{},
			signature: make([]byte, 64),
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := VerifyRingtailSignature(tt.publicKey, tt.message, tt.signature)
			require.Equal(tt.expected, result)
		})
	}
}

// TestHybridBLSRTSignatureVerifyNetworkIDMismatch tests network ID validation
func TestHybridBLSRTSignatureVerifyNetworkIDMismatch(t *testing.T) {
	require := require.New(t)

	msg := &UnsignedMessage{
		NetworkID: 1,
	}

	sig := &HybridBLSRTSignature{}

	validators := CanonicalValidatorSet{}

	err := sig.Verify(msg, 2, validators, 2, 3) // Network ID 2 != 1
	require.ErrorIs(err, ErrWrongNetworkID)
}

// TestHybridBLSRTSignatureVerifyInvalidBitSet tests invalid bitset validation
func TestHybridBLSRTSignatureVerifyInvalidBitSet(t *testing.T) {
	require := require.New(t)

	msg := &UnsignedMessage{
		NetworkID: 1,
	}

	// Create a signature with padded bitset (invalid)
	sig := &HybridBLSRTSignature{
		Signers: []byte{0x00, 0x01}, // Extra leading zero is invalid
	}

	validators := CanonicalValidatorSet{
		TotalWeight: 100,
	}

	err := sig.Verify(msg, 1, validators, 2, 3)
	require.ErrorIs(err, ErrInvalidBitSet)
}

// TestHybridBLSRTSignatureRingtailKeyMismatch tests missing RT public keys
func TestHybridBLSRTSignatureRingtailKeyMismatch(t *testing.T) {
	require := require.New(t)

	// Generate BLS key pair for testing
	sk, err := bls.NewSecretKey()
	require.NoError(err)
	pk := bls.PublicFromSecretKey(sk)
	pkBytes := bls.PublicKeyToUncompressedBytes(pk)

	msg := &UnsignedMessage{
		NetworkID:     1,
		SourceChainID: ids.GenerateTestID(),
	}
	msg.bytes = []byte("test message") // Set bytes for signing

	// Create validators with the BLS key
	validators := CanonicalValidatorSet{
		Validators: []*Validator{
			{
				PublicKey:      pk,
				PublicKeyBytes: pkBytes,
				Weight:         100,
				NodeIDs:        []ids.NodeID{ids.GenerateTestNodeID()},
			},
		},
		TotalWeight: 100,
	}

	// Create bitset indicating validator 0 signed
	bits := set.NewBits()
	bits.Add(0)

	// Sign with BLS using SecretKey.Sign() method
	blsSig, err := sk.Sign(msg.Bytes())
	require.NoError(err)
	var blsSigBytes [bls.SignatureLen]byte
	copy(blsSigBytes[:], bls.SignatureToBytes(blsSig))

	// Create signature with missing RT public keys
	sig := &HybridBLSRTSignature{
		Signers:            bits.Bytes(),
		BLSSignature:       blsSigBytes,
		RingtailSignature:  make([]byte, 64),
		RingtailPublicKeys: [][]byte{}, // Missing RT keys
	}

	err = sig.Verify(msg, 1, validators, 2, 3)
	require.ErrorContains(err, "Ringtail")
	require.ErrorContains(err, "missing")
}

// TestHybridBLSRTSignatureCodecRoundTrip tests serialization
func TestHybridBLSRTSignatureCodecRoundTrip(t *testing.T) {
	require := require.New(t)

	original := &HybridBLSRTSignature{
		Signers:      []byte{0x07}, // 3 signers
		BLSSignature: [bls.SignatureLen]byte{1, 2, 3, 4, 5},
		RingtailSignature: []byte{
			0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
			0x90, 0xA0, 0xB0, 0xC0, 0xD0, 0xE0, 0xF0, 0x00,
			0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
			0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x01,
			0x12, 0x23, 0x34, 0x45, 0x56, 0x67, 0x78, 0x89,
			0x9A, 0xAB, 0xBC, 0xCD, 0xDE, 0xEF, 0xF0, 0x02,
			0x13, 0x24, 0x35, 0x46, 0x57, 0x68, 0x79, 0x8A,
			0x9B, 0xAC, 0xBD, 0xCE, 0xDF, 0xE0, 0xF1, 0x03,
		},
		RingtailPublicKeys: [][]byte{
			make([]byte, 32),
			make([]byte, 32),
			make([]byte, 32),
		},
	}

	// Encode
	encoded, err := Codec.Marshal(CodecVersion, original)
	require.NoError(err)

	// Decode
	decoded := &HybridBLSRTSignature{}
	_, err = Codec.Unmarshal(encoded, decoded)
	require.NoError(err)

	// Verify
	require.Equal(original.Signers, decoded.Signers)
	require.Equal(original.BLSSignature, decoded.BLSSignature)
	require.Equal(original.RingtailSignature, decoded.RingtailSignature)
	require.Equal(len(original.RingtailPublicKeys), len(decoded.RingtailPublicKeys))
}

// TestValidatorWithRingtailKey tests validator with both BLS and RT keys
func TestValidatorWithRingtailKey(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()

	// Create validator data with both key types
	vdrData := &ValidatorData{
		NodeID:         nodeID,
		PublicKey:      make([]byte, 48), // BLS compressed key
		RingtailPubKey: make([]byte, 32), // Ringtail key
		Weight:         100,
	}

	require.Equal(nodeID, vdrData.NodeID)
	require.NotNil(vdrData.PublicKey)
	require.NotNil(vdrData.RingtailPubKey)
	require.Equal(uint64(100), vdrData.Weight)
}

// TestRingtailSignatureLenConstant tests the Ringtail signature length constant
// Note: This is a legacy constant. Ringtail signatures have variable length based on parameters.
func TestRingtailSignatureLenConstant(t *testing.T) {
	require := require.New(t)

	// Legacy constant (retained for backwards compatibility)
	require.Equal(3309, RingtailSignatureLen)
}

// =============================================================================
// Warp 1.5: RingtailSignature Tests (RT-only, replaces BLS)
// =============================================================================

// TestRingtailSignatureNumSigners tests the NumSigners method for RT-only signatures
func TestRingtailSignatureNumSigners(t *testing.T) {
	require := require.New(t)

	tests := []struct {
		name           string
		signers        []byte
		expectedNum    int
		expectedErrMsg string
	}{
		{
			name:        "empty signers",
			signers:     []byte{},
			expectedNum: 0,
		},
		{
			name:        "single signer",
			signers:     []byte{0x01},
			expectedNum: 1,
		},
		{
			name:        "five signers",
			signers:     []byte{0x1F}, // bits 0, 1, 2, 3, 4 set
			expectedNum: 5,
		},
		{
			name:        "eight signers",
			signers:     []byte{0xFF},
			expectedNum: 8,
		},
		{
			name:        "padded bitset (invalid)",
			signers:     []byte{0x00, 0x01}, // Extra leading zero
			expectedNum: 0,
			expectedErrMsg: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig := &RingtailSignature{
				Signers: tt.signers,
			}

			num, err := sig.NumSigners()
			if tt.expectedErrMsg != "" {
				require.Error(err)
			} else {
				require.NoError(err)
				require.Equal(tt.expectedNum, num)
			}
		})
	}
}

// TestRingtailSignatureString tests the String method for RT-only signatures
func TestRingtailSignatureString(t *testing.T) {
	require := require.New(t)

	sig := &RingtailSignature{
		Signers:   []byte{0x07}, // 3 signers
		Signature: make([]byte, 64),
	}

	str := sig.String()
	require.Contains(str, "RingtailSignature")
	require.Contains(str, "Signers")
	require.Contains(str, "Sig")
}

// TestRingtailSignatureVerifyNetworkIDMismatch tests network ID validation for RT-only
func TestRingtailSignatureVerifyNetworkIDMismatch(t *testing.T) {
	require := require.New(t)

	msg := &UnsignedMessage{
		NetworkID: 1,
	}

	sig := &RingtailSignature{}

	validators := CanonicalValidatorSet{}

	err := sig.Verify(msg, 2, validators, 2, 3) // Network ID 2 != 1
	require.ErrorIs(err, ErrWrongNetworkID)
}

// TestRingtailSignatureVerifyInvalidBitSet tests invalid bitset validation for RT-only
func TestRingtailSignatureVerifyInvalidBitSet(t *testing.T) {
	require := require.New(t)

	msg := &UnsignedMessage{
		NetworkID: 1,
	}

	// Create a signature with padded bitset (invalid)
	sig := &RingtailSignature{
		Signers: []byte{0x00, 0x01}, // Extra leading zero is invalid
	}

	validators := CanonicalValidatorSet{
		TotalWeight: 100,
	}

	err := sig.Verify(msg, 1, validators, 2, 3)
	require.ErrorIs(err, ErrInvalidBitSet)
}

// TestRingtailSignatureCodecRoundTrip tests serialization for RT-only signatures
func TestRingtailSignatureCodecRoundTrip(t *testing.T) {
	require := require.New(t)

	original := &RingtailSignature{
		Signers:   []byte{0x0F}, // 4 signers
		Signature: make([]byte, 256), // Variable length Ringtail signature
	}
	// Fill with test data
	for i := range original.Signature {
		original.Signature[i] = byte(i)
	}

	// Encode
	encoded, err := Codec.Marshal(CodecVersion, original)
	require.NoError(err)

	// Decode
	decoded := &RingtailSignature{}
	_, err = Codec.Unmarshal(encoded, decoded)
	require.NoError(err)

	// Verify
	require.Equal(original.Signers, decoded.Signers)
	require.Equal(original.Signature, decoded.Signature)
}

// TestRingtailConstants verifies Ringtail protocol constants match the implementation
func TestRingtailConstants(t *testing.T) {
	require := require.New(t)

	// Verify constants match github.com/luxfi/ringtail/sign/config.go
	require.Equal(uint64(0x1000000004A01), uint64(RingtailQ))
	require.Equal(8, RingtailM)
	require.Equal(7, RingtailN)
	require.Equal(23, RingtailKappa)
	require.Equal(48, RingtailDbar)
	require.Equal(32, RingtailKeySize)
}

// TestMinHelper tests the min helper function
func TestMinHelper(t *testing.T) {
	require := require.New(t)

	require.Equal(1, min(1, 2))
	require.Equal(1, min(2, 1))
	require.Equal(5, min(5, 5))
	require.Equal(0, min(0, 10))
	require.Equal(-5, min(-5, 0))
}

// =============================================================================
// Warp 1.5: EncryptedWarpPayload Tests (ML-KEM + AES-256-GCM)
// =============================================================================

// TestEncryptedWarpPayloadRoundTrip tests encrypt/decrypt cycle
func TestEncryptedWarpPayloadRoundTrip(t *testing.T) {
	require := require.New(t)

	// Generate test ML-KEM key pair (placeholder keys)
	recipientPubKey := make([]byte, MLKEM768PublicKeyLen)
	recipientPrivKey := make([]byte, 2400) // ML-KEM-768 private key
	for i := range recipientPubKey {
		recipientPubKey[i] = byte(i)
	}
	for i := range recipientPrivKey {
		recipientPrivKey[i] = byte(i)
	}

	recipientKeyID := []byte("test-key-id")
	plaintext := []byte("Hello, cross-chain world! This is a confidential message.")

	// Encrypt
	encrypted, err := EncryptWarpPayload(plaintext, recipientPubKey, recipientKeyID)
	require.NoError(err)
	require.NotNil(encrypted)
	require.Equal(MLKEM768CiphertextLen, len(encrypted.EncapsulatedKey))
	require.Equal(AESGCMNonceLen, len(encrypted.Nonce))
	require.Equal(recipientKeyID, encrypted.RecipientKeyID)

	// Decrypt (placeholder implementation)
	decrypted, err := encrypted.Decrypt(recipientPrivKey)
	require.NoError(err)
	require.Equal(plaintext, decrypted)
}

// TestEncryptedWarpPayloadInvalidPubKeyLength tests public key validation
func TestEncryptedWarpPayloadInvalidPubKeyLength(t *testing.T) {
	require := require.New(t)

	// Wrong public key length
	badPubKey := make([]byte, 100) // Should be MLKEM768PublicKeyLen (1184)
	recipientKeyID := []byte("test-key-id")
	plaintext := []byte("test")

	_, err := EncryptWarpPayload(plaintext, badPubKey, recipientKeyID)
	require.Error(err)
	require.Contains(err.Error(), "invalid ML-KEM public key length")
}

// TestEncryptedWarpPayloadDecryptInvalidCiphertext tests ciphertext validation
func TestEncryptedWarpPayloadDecryptInvalidCiphertext(t *testing.T) {
	require := require.New(t)

	privKey := make([]byte, 2400)

	tests := []struct {
		name      string
		payload   *EncryptedWarpPayload
		expectErr string
	}{
		{
			name: "wrong encapsulated key length",
			payload: &EncryptedWarpPayload{
				EncapsulatedKey: make([]byte, 100), // Wrong length
				Nonce:           make([]byte, AESGCMNonceLen),
				Ciphertext:      make([]byte, 100),
			},
			expectErr: "invalid encapsulated key length",
		},
		{
			name: "wrong nonce length",
			payload: &EncryptedWarpPayload{
				EncapsulatedKey: make([]byte, MLKEM768CiphertextLen),
				Nonce:           make([]byte, 8), // Wrong length
				Ciphertext:      make([]byte, 100),
			},
			expectErr: "invalid nonce length",
		},
		{
			name: "ciphertext too short",
			payload: &EncryptedWarpPayload{
				EncapsulatedKey: make([]byte, MLKEM768CiphertextLen),
				Nonce:           make([]byte, AESGCMNonceLen),
				Ciphertext:      make([]byte, 5), // Too short for auth tag
			},
			expectErr: "ciphertext too short",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.payload.Decrypt(privKey)
			require.Error(err)
			require.Contains(err.Error(), tt.expectErr)
		})
	}
}

// TestEncryptedWarpPayloadSize tests size calculation
func TestEncryptedWarpPayloadSize(t *testing.T) {
	require := require.New(t)

	payload := &EncryptedWarpPayload{
		EncapsulatedKey: make([]byte, MLKEM768CiphertextLen),
		Nonce:           make([]byte, AESGCMNonceLen),
		Ciphertext:      make([]byte, 100),
		RecipientKeyID:  []byte("key-id-123"),
	}

	expectedSize := MLKEM768CiphertextLen + AESGCMNonceLen + 100 + len("key-id-123")
	require.Equal(expectedSize, payload.Size())
}

// TestEncryptedWarpPayloadString tests string representation
func TestEncryptedWarpPayloadString(t *testing.T) {
	require := require.New(t)

	payload := &EncryptedWarpPayload{
		EncapsulatedKey: make([]byte, MLKEM768CiphertextLen),
		Nonce:           make([]byte, AESGCMNonceLen),
		Ciphertext:      make([]byte, 100),
		RecipientKeyID:  []byte("my-key-id"),
	}

	str := payload.String()
	require.Contains(str, "EncryptedWarpPayload")
	require.Contains(str, "KeyID")
	require.Contains(str, "bytes")
}

// TestEncryptedWarpPayloadCodecRoundTrip tests serialization
func TestEncryptedWarpPayloadCodecRoundTrip(t *testing.T) {
	require := require.New(t)

	original := &EncryptedWarpPayload{
		EncapsulatedKey: make([]byte, MLKEM768CiphertextLen),
		Nonce:           make([]byte, AESGCMNonceLen),
		Ciphertext:      []byte("encrypted data here"),
		RecipientKeyID:  []byte("recipient-123"),
	}
	// Fill encapsulated key with test data
	for i := range original.EncapsulatedKey {
		original.EncapsulatedKey[i] = byte(i % 256)
	}

	// Encode
	encoded, err := Codec.Marshal(CodecVersion, original)
	require.NoError(err)

	// Decode
	decoded := &EncryptedWarpPayload{}
	_, err = Codec.Unmarshal(encoded, decoded)
	require.NoError(err)

	// Verify
	require.Equal(original.EncapsulatedKey, decoded.EncapsulatedKey)
	require.Equal(original.Nonce, decoded.Nonce)
	require.Equal(original.Ciphertext, decoded.Ciphertext)
	require.Equal(original.RecipientKeyID, decoded.RecipientKeyID)
}

// TestMLKEMConstants verifies ML-KEM constants are correct per FIPS 203
func TestMLKEMConstants(t *testing.T) {
	require := require.New(t)

	// FIPS 203 ML-KEM-768 parameters
	require.Equal(1088, MLKEM768CiphertextLen)
	require.Equal(1184, MLKEM768PublicKeyLen)
	require.Equal(32, MLKEM768SharedSecretLen)
	require.Equal(1568, MLKEM1024CiphertextLen)

	// AES-GCM parameters
	require.Equal(12, AESGCMNonceLen)
	require.Equal(16, AESGCMTagLen)
}
