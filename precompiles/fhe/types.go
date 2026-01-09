// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package fhe provides EVM precompiles for Fully Homomorphic Encryption operations.
// These precompiles expose FHE operations to smart contracts running on the C-Chain.
//
// Architecture:
//   - Precompiles at 0x0080-0x00FF for FHE operations
//   - Ciphertexts stored by 32-byte handle
//   - Operations consume gas proportional to computational cost
//   - Threshold decryption via Warp messaging to T-Chain
package fhe

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/luxfi/fhe"
)

// Precompile addresses for FHE operations
const (
	// Arithmetic operations
	FHEAddAddress = 0x0080
	FHESubAddress = 0x0081
	FHEMulAddress = 0x0082
	FHEDivAddress = 0x0083
	FHERemAddress = 0x0084
	FHENegAddress = 0x0085

	// Comparison operations
	FHELeAddress = 0x0086
	FHELtAddress = 0x0087
	FHEGeAddress = 0x0088
	FHEGtAddress = 0x0089
	FHEEqAddress = 0x008A
	FHENeAddress = 0x008B

	// Min/Max operations
	FHEMinAddress = 0x008C
	FHEMaxAddress = 0x008D

	// Bitwise operations
	FHEAndAddress  = 0x0090
	FHEOrAddress   = 0x0091
	FHEXorAddress  = 0x0092
	FHENotAddress  = 0x0093
	FHEShlAddress  = 0x0094
	FHEShrAddress  = 0x0095
	FHERotlAddress = 0x0096
	FHERotrAddress = 0x0097

	// Encryption/Decryption
	TrivialEncryptAddress   = 0x00A0
	VerifyCiphertextAddress = 0x00A1
	CastAddress             = 0x00A2
	ReencryptAddress        = 0x00A3
	OptReencryptAddress     = 0x00A4
	DecryptAddress          = 0x00A5

	// Select (conditional)
	FHESelectAddress = 0x00B0
)

// FheUintType represents encrypted integer types matching the Solidity interface
type FheUintType uint8

const (
	FheBool    FheUintType = 0
	FheUint4   FheUintType = 1
	FheUint8   FheUintType = 2
	FheUint16  FheUintType = 3
	FheUint32  FheUintType = 4
	FheUint64  FheUintType = 5
	FheUint128 FheUintType = 6
	FheUint160 FheUintType = 7 // Ethereum address
	FheUint256 FheUintType = 8
)

// NumBits returns the bit width for the type
func (t FheUintType) NumBits() int {
	switch t {
	case FheBool:
		return 1
	case FheUint4:
		return 4
	case FheUint8:
		return 8
	case FheUint16:
		return 16
	case FheUint32:
		return 32
	case FheUint64:
		return 64
	case FheUint128:
		return 128
	case FheUint160:
		return 160
	case FheUint256:
		return 256
	default:
		return 0
	}
}

// String returns the type name
func (t FheUintType) String() string {
	switch t {
	case FheBool:
		return "ebool"
	case FheUint4:
		return "euint4"
	case FheUint8:
		return "euint8"
	case FheUint16:
		return "euint16"
	case FheUint32:
		return "euint32"
	case FheUint64:
		return "euint64"
	case FheUint128:
		return "euint128"
	case FheUint160:
		return "euint160"
	case FheUint256:
		return "euint256"
	default:
		return "unknown"
	}
}

// ToLibType converts precompile type to the fhe library type
func (t FheUintType) ToLibType() fhe.FheUintType {
	switch t {
	case FheBool:
		return fhe.FheBool
	case FheUint4:
		return fhe.FheUint4
	case FheUint8:
		return fhe.FheUint8
	case FheUint16:
		return fhe.FheUint16
	case FheUint32:
		return fhe.FheUint32
	case FheUint64:
		return fhe.FheUint64
	case FheUint128:
		return fhe.FheUint128
	case FheUint160:
		return fhe.FheUint160
	case FheUint256:
		return fhe.FheUint256
	default:
		return fhe.FheBool
	}
}

// Handle is a 32-byte identifier for a stored ciphertext
type Handle [32]byte

// Empty returns true if handle is all zeros
func (h Handle) Empty() bool {
	for _, b := range h {
		if b != 0 {
			return false
		}
	}
	return true
}

// CiphertextMeta stores metadata about a ciphertext
type CiphertextMeta struct {
	Type       FheUintType
	Handle     Handle
	Owner      [20]byte // Ethereum address that created this ciphertext
	Serialized []byte   // Serialized ciphertext data
}

// Gas costs for FHE operations (in gas units)
// These are approximate costs based on computational complexity
var GasCosts = map[uint64]uint64{
	FHEAddAddress: 50_000,
	FHESubAddress: 50_000,
	FHEMulAddress: 150_000,
	FHEDivAddress: 500_000,
	FHERemAddress: 500_000,
	FHENegAddress: 30_000,

	FHELeAddress: 200_000,
	FHELtAddress: 200_000,
	FHEGeAddress: 200_000,
	FHEGtAddress: 200_000,
	FHEEqAddress: 200_000,
	FHENeAddress: 200_000,

	FHEMinAddress: 250_000,
	FHEMaxAddress: 250_000,

	FHEAndAddress:  50_000,
	FHEOrAddress:   50_000,
	FHEXorAddress:  50_000,
	FHENotAddress:  30_000,
	FHEShlAddress:  100_000,
	FHEShrAddress:  100_000,
	FHERotlAddress: 100_000,
	FHERotrAddress: 100_000,

	TrivialEncryptAddress:   20_000,
	VerifyCiphertextAddress: 100_000,
	CastAddress:             50_000,
	ReencryptAddress:        200_000,
	OptReencryptAddress:     150_000,
	DecryptAddress:          1_000_000, // High cost - requires threshold decryption

	FHESelectAddress: 300_000,
}

// Errors
var (
	ErrInvalidInput          = errors.New("fhe: invalid input")
	ErrInvalidHandle         = errors.New("fhe: invalid ciphertext handle")
	ErrHandleNotFound        = errors.New("fhe: ciphertext handle not found")
	ErrTypeMismatch          = errors.New("fhe: operand type mismatch")
	ErrInsufficientLevels    = errors.New("fhe: insufficient multiplicative levels")
	ErrUnauthorized          = errors.New("fhe: unauthorized access to ciphertext")
	ErrDecryptionFailed      = errors.New("fhe: threshold decryption failed")
	ErrOperationNotSupported = errors.New("fhe: operation not supported")
	ErrInvalidCiphertext     = errors.New("fhe: invalid ciphertext data")
)

// ParseHandle extracts a 32-byte handle from input bytes at given offset
func ParseHandle(input []byte, offset int) (Handle, error) {
	if len(input) < offset+32 {
		return Handle{}, fmt.Errorf("%w: need 32 bytes for handle at offset %d", ErrInvalidInput, offset)
	}
	var h Handle
	copy(h[:], input[offset:offset+32])
	return h, nil
}

// ParseType extracts the FHE type from input (last byte of a 32-byte slot)
func ParseType(input []byte, offset int) (FheUintType, error) {
	if len(input) < offset+32 {
		return 0, fmt.Errorf("%w: need 32 bytes for type at offset %d", ErrInvalidInput, offset)
	}
	// Type is in the last byte of the slot (big-endian uint256)
	return FheUintType(input[offset+31]), nil
}

// ParseUint64 extracts a uint64 from 32-byte slot (big-endian)
func ParseUint64(input []byte, offset int) (uint64, error) {
	if len(input) < offset+32 {
		return 0, fmt.Errorf("%w: need 32 bytes for uint64 at offset %d", ErrInvalidInput, offset)
	}
	// Value is in the last 8 bytes of the slot (big-endian uint256)
	return binary.BigEndian.Uint64(input[offset+24 : offset+32]), nil
}

// EncodeHandle writes a handle to a 32-byte output slice
func EncodeHandle(h Handle) []byte {
	return h[:]
}

// EncodeBool writes a boolean result as 32-byte output
func EncodeBool(b bool) []byte {
	result := make([]byte, 32)
	if b {
		result[31] = 1
	}
	return result
}

// EncodeUint256 encodes bytes as 32-byte output
func EncodeUint256(data []byte) []byte {
	result := make([]byte, 32)
	if len(data) > 32 {
		data = data[len(data)-32:]
	}
	copy(result[32-len(data):], data)
	return result
}
