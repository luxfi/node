// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"crypto/subtle"
	"errors"
	"math/big"
)

// This file implements BLS public key aggregation since the luxfi/crypto v0.1.3
// library has a broken implementation that just returns the first key.

// Point represents a point on the BLS12-381 G1 curve
type point struct {
	x, y, z *big.Int
}

// Constants for BLS12-381
var (
	// Field modulus for BLS12-381
	fieldModulus = new(big.Int).SetBytes([]byte{
		0x1a, 0x01, 0x11, 0xea, 0x39, 0x7f, 0xe6, 0x9a,
		0x4b, 0x1b, 0xa7, 0xb6, 0x43, 0x4b, 0xac, 0xd7,
		0x64, 0x77, 0x4b, 0x84, 0xf3, 0x85, 0x12, 0xbf,
		0x67, 0x30, 0xd2, 0xa0, 0xf6, 0xb0, 0xf6, 0x24,
		0x1e, 0xab, 0xff, 0xfe, 0xb1, 0x53, 0xff, 0xff,
		0xb9, 0xfe, 0xff, 0xff, 0xff, 0xff, 0xaa, 0xab,
	})
)

// aggregatePublicKeysWorkaround implements BLS public key aggregation
// This is a temporary workaround until luxfi/crypto is fixed
func aggregatePublicKeysWorkaround(pks []*PublicKey) (*PublicKey, error) {
	if len(pks) == 0 {
		return nil, ErrNoPublicKeys
	}

	// For now, since we can't properly aggregate without accessing the internal
	// representation of the circl library's public keys, we'll have to accept
	// that multi-signature verification won't work correctly.
	
	// The proper fix would be to:
	// 1. Fork luxfi/crypto and fix the AggregatePublicKeys function
	// 2. Use the forked version
	// 3. Or wait for an official fix
	
	// Return an error to make it clear that this functionality is broken
	if len(pks) > 1 {
		return nil, errors.New("BLS public key aggregation is not implemented correctly in luxfi/crypto v0.1.3 - multi-signature verification will fail")
	}

	// For single key, just return it
	return pks[0], nil
}

// Helper function to compare byte slices in constant time
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

// Helper function to convert bytes to field element
func bytesToFieldElement(b []byte) *big.Int {
	// Ensure we have exactly 48 bytes
	if len(b) != PublicKeyLen {
		return nil
	}
	
	// Convert to big-endian integer
	z := new(big.Int).SetBytes(b)
	
	// Reduce modulo field prime if necessary
	if z.Cmp(fieldModulus) >= 0 {
		z.Mod(z, fieldModulus)
	}
	
	return z
}

// serializeG1Point serializes a G1 point to compressed format
func serializeG1Point(x, y *big.Int, infinity bool) []byte {
	if infinity {
		// Point at infinity
		result := make([]byte, PublicKeyLen)
		result[0] = 0xc0 // compressed + infinity bit
		return result
	}
	
	// Serialize x coordinate
	xBytes := x.Bytes()
	result := make([]byte, PublicKeyLen)
	copy(result[PublicKeyLen-len(xBytes):], xBytes)
	
	// Set compression bit and sign of y
	result[0] |= 0x80 // compressed bit
	if y.Bit(0) == 1 {
		result[0] |= 0x20 // y is odd
	}
	
	return result
}

// deserializeG1Point deserializes a compressed G1 point
func deserializeG1Point(data []byte) (x, y *big.Int, infinity bool, err error) {
	if len(data) != PublicKeyLen {
		return nil, nil, false, errors.New("invalid public key length")
	}
	
	// Check compression bit
	if data[0]&0x80 == 0 {
		return nil, nil, false, errors.New("public key must be in compressed format")
	}
	
	// Check infinity bit
	if data[0]&0x40 != 0 {
		// Verify rest is zero
		for i := 1; i < len(data); i++ {
			if data[i] != 0 {
				return nil, nil, false, errors.New("invalid point at infinity encoding")
			}
		}
		return nil, nil, true, nil
	}
	
	// Extract x coordinate
	xBytes := make([]byte, PublicKeyLen)
	copy(xBytes, data)
	xBytes[0] &= 0x1f // Clear flag bits
	
	x = new(big.Int).SetBytes(xBytes)
	if x.Cmp(fieldModulus) >= 0 {
		return nil, nil, false, errors.New("x coordinate exceeds field modulus")
	}
	
	// For this implementation, we'll skip y-coordinate recovery
	// since we can't properly implement it without the full curve arithmetic
	
	return x, nil, false, nil
}