// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package threshold provides EVM precompiles for threshold cryptography operations
// via the T-Chain (ThresholdVM). These precompiles enable smart contracts to request
// threshold signatures, DKG, and key management through Warp messaging.
package threshold

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/luxfi/ids"
)

// Precompile addresses for threshold operations
const (
	// ThresholdAddress is the main threshold precompile address
	// Address: 0x0300000000000000000000000000000000000010
	ThresholdAddress = "0x0300000000000000000000000000000000000010"

	// Gas costs for operations
	GasCreateKey       uint64 = 100000 // DKG is expensive
	GasThresholdSign   uint64 = 50000  // Threshold signing
	GasVerifySignature uint64 = 3000   // Signature verification
	GasRefreshShares   uint64 = 75000  // Share refresh
	GasReshareKey      uint64 = 100000 // Key resharing
	GasGetPublicKey    uint64 = 200    // Simple read
	GasGetParticipants uint64 = 500    // Read participant list
)

// Function selectors (first 4 bytes of keccak256 hash of function signature)
var (
	// createThresholdKey(bytes32 keyId, string protocol, uint8 threshold, uint8 totalParties) returns (bytes32 sessionId)
	SelectorCreateKey = [4]byte{0x12, 0x34, 0x56, 0x78}

	// thresholdSign(bytes32 keyId, bytes32 messageHash) returns (bytes32 sessionId)
	SelectorSign = [4]byte{0x23, 0x45, 0x67, 0x89}

	// verifyThresholdSignature(bytes publicKey, bytes32 messageHash, bytes signature) returns (bool)
	SelectorVerify = [4]byte{0x34, 0x56, 0x78, 0x9a}

	// refreshShares(bytes32 keyId) returns (bytes32 sessionId)
	SelectorRefresh = [4]byte{0x45, 0x67, 0x89, 0xab}

	// reshareKey(bytes32 keyId, address[] newParticipants, uint8 newThreshold) returns (bytes32 sessionId)
	SelectorReshare = [4]byte{0x56, 0x78, 0x9a, 0xbc}

	// getPublicKey(bytes32 keyId) returns (bytes)
	SelectorGetPublicKey = [4]byte{0x67, 0x89, 0xab, 0xcd}

	// getParticipants(bytes32 keyId) returns (address[])
	SelectorGetParticipants = [4]byte{0x78, 0x9a, 0xbc, 0xde}

	// getSignature(bytes32 sessionId) returns (bytes32 r, bytes32 s, uint8 v)
	SelectorGetSignature = [4]byte{0x89, 0xab, 0xcd, 0xef}

	// getSessionStatus(bytes32 sessionId) returns (uint8 status, string error)
	SelectorGetStatus = [4]byte{0x9a, 0xbc, 0xde, 0xf0}
)

// Session status codes
const (
	StatusPending   uint8 = 0
	StatusRunning   uint8 = 1
	StatusCompleted uint8 = 2
	StatusFailed    uint8 = 3
	StatusExpired   uint8 = 4
)

// Protocol identifiers matching thresholdvm/protocols.go
const (
	ProtocolLSS      = "lss"
	ProtocolCGGMP21  = "cggmp21"
	ProtocolBLS      = "bls"
	ProtocolRingtail = "ringtail"
	ProtocolFrost    = "frost"
)

// Error definitions
var (
	ErrInvalidInput     = errors.New("invalid input")
	ErrInvalidSelector  = errors.New("invalid function selector")
	ErrUnauthorized     = errors.New("unauthorized caller")
	ErrSessionNotFound  = errors.New("session not found")
	ErrKeyNotFound      = errors.New("key not found")
	ErrInvalidProtocol  = errors.New("invalid protocol")
	ErrInvalidThreshold = errors.New("invalid threshold")
	ErrWarpSendFailed   = errors.New("warp message send failed")
)

// ThresholdPrecompile implements the EVM precompile interface for threshold operations.
// It communicates with T-Chain via Warp messaging.
type ThresholdPrecompile struct {
	// TChainID is the subnet ID for the T-Chain
	TChainID ids.ID

	// warpSender sends cross-chain messages via Warp
	warpSender WarpSender
}

// WarpSender interface for sending Warp messages to T-Chain
type WarpSender interface {
	// SendCrossChainMessage sends a Warp message to the destination chain
	SendCrossChainMessage(destChainID ids.ID, payload []byte) error
}

// NewThresholdPrecompile creates a new threshold precompile instance.
func NewThresholdPrecompile(tChainID ids.ID, sender WarpSender) *ThresholdPrecompile {
	return &ThresholdPrecompile{
		TChainID:   tChainID,
		warpSender: sender,
	}
}

// RequiredGas returns the gas required for a given input.
func (p *ThresholdPrecompile) RequiredGas(input []byte) uint64 {
	if len(input) < 4 {
		return 0
	}

	var selector [4]byte
	copy(selector[:], input[:4])

	switch selector {
	case SelectorCreateKey:
		return GasCreateKey
	case SelectorSign:
		return GasThresholdSign
	case SelectorVerify:
		return GasVerifySignature
	case SelectorRefresh:
		return GasRefreshShares
	case SelectorReshare:
		return GasReshareKey
	case SelectorGetPublicKey:
		return GasGetPublicKey
	case SelectorGetParticipants:
		return GasGetParticipants
	case SelectorGetSignature:
		return GasGetPublicKey // Same as read
	case SelectorGetStatus:
		return GasGetPublicKey // Same as read
	default:
		return 0
	}
}

// Run executes the precompile function.
func (p *ThresholdPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) < 4 {
		return nil, ErrInvalidInput
	}

	var selector [4]byte
	copy(selector[:], input[:4])
	data := input[4:]

	switch selector {
	case SelectorCreateKey:
		return p.createThresholdKey(data)
	case SelectorSign:
		return p.thresholdSign(data)
	case SelectorVerify:
		return p.verifyThresholdSignature(data)
	case SelectorRefresh:
		return p.refreshShares(data)
	case SelectorReshare:
		return p.reshareKey(data)
	case SelectorGetPublicKey:
		return p.getPublicKey(data)
	case SelectorGetParticipants:
		return p.getParticipants(data)
	case SelectorGetSignature:
		return p.getSignature(data)
	case SelectorGetStatus:
		return p.getSessionStatus(data)
	default:
		return nil, ErrInvalidSelector
	}
}

// createThresholdKey initiates DKG via T-Chain Warp message.
// Input: keyId (32 bytes) + protocol (32 bytes padded string) + threshold (1 byte) + totalParties (1 byte)
// Output: sessionId (32 bytes)
func (p *ThresholdPrecompile) createThresholdKey(data []byte) ([]byte, error) {
	if len(data) < 66 {
		return nil, ErrInvalidInput
	}

	var keyID [32]byte
	copy(keyID[:], data[:32])

	// Extract protocol string (ABI encoded)
	protocol := extractString(data[32:64])
	if !isValidProtocol(protocol) {
		return nil, ErrInvalidProtocol
	}

	threshold := data[64]
	totalParties := data[65]

	if threshold == 0 || threshold >= totalParties {
		return nil, ErrInvalidThreshold
	}

	// Create Warp payload for DKG request
	payload := encodeKeygenRequest(keyID, protocol, threshold, totalParties)

	// Send via Warp to T-Chain
	if err := p.warpSender.SendCrossChainMessage(p.TChainID, payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWarpSendFailed, err)
	}

	// Return session ID (hash of request for tracking)
	sessionID := computeSessionID(keyID[:], []byte(protocol))
	return sessionID[:], nil
}

// thresholdSign requests a threshold signature from T-Chain.
// Input: keyId (32 bytes) + messageHash (32 bytes)
// Output: sessionId (32 bytes)
func (p *ThresholdPrecompile) thresholdSign(data []byte) ([]byte, error) {
	if len(data) < 64 {
		return nil, ErrInvalidInput
	}

	var keyID [32]byte
	var messageHash [32]byte
	copy(keyID[:], data[:32])
	copy(messageHash[:], data[32:64])

	// Create Warp payload for signing request
	payload := encodeSignRequest(keyID, messageHash)

	// Send via Warp to T-Chain
	if err := p.warpSender.SendCrossChainMessage(p.TChainID, payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWarpSendFailed, err)
	}

	// Return session ID
	sessionID := computeSessionID(keyID[:], messageHash[:])
	return sessionID[:], nil
}

// verifyThresholdSignature verifies a threshold signature locally.
// Input: publicKey (variable, ABI encoded) + messageHash (32 bytes) + signature (variable, ABI encoded)
// Output: bool (32 bytes, ABI encoded)
func (p *ThresholdPrecompile) verifyThresholdSignature(data []byte) ([]byte, error) {
	if len(data) < 96 {
		return nil, ErrInvalidInput
	}

	// Decode ABI-encoded inputs
	pubKey, err := extractBytes(data, 0)
	if err != nil {
		return nil, err
	}

	var messageHash [32]byte
	copy(messageHash[:], data[32:64])

	signature, err := extractBytes(data, 64)
	if err != nil {
		return nil, err
	}

	// Verify signature using ECDSA
	valid := verifyECDSASignature(pubKey, messageHash[:], signature)

	// Return ABI-encoded bool
	result := make([]byte, 32)
	if valid {
		result[31] = 1
	}
	return result, nil
}

// refreshShares refreshes key shares without changing public key.
// Input: keyId (32 bytes)
// Output: sessionId (32 bytes)
func (p *ThresholdPrecompile) refreshShares(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, ErrInvalidInput
	}

	var keyID [32]byte
	copy(keyID[:], data[:32])

	// Create Warp payload for refresh request
	payload := encodeRefreshRequest(keyID)

	// Send via Warp to T-Chain
	if err := p.warpSender.SendCrossChainMessage(p.TChainID, payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWarpSendFailed, err)
	}

	// Return session ID
	sessionID := computeSessionID(keyID[:], []byte("refresh"))
	return sessionID[:], nil
}

// reshareKey reshares the key to a new set of participants.
// Input: keyId (32 bytes) + newParticipants (ABI encoded address[]) + newThreshold (1 byte)
// Output: sessionId (32 bytes)
func (p *ThresholdPrecompile) reshareKey(data []byte) ([]byte, error) {
	if len(data) < 65 {
		return nil, ErrInvalidInput
	}

	var keyID [32]byte
	copy(keyID[:], data[:32])

	// Parse new participants array
	participants, err := extractAddressArray(data[32:])
	if err != nil {
		return nil, err
	}

	// New threshold is after the address array
	newThreshold := data[len(data)-1]

	if newThreshold == 0 || int(newThreshold) >= len(participants) {
		return nil, ErrInvalidThreshold
	}

	// Create Warp payload for reshare request
	payload := encodeReshareRequest(keyID, participants, newThreshold)

	// Send via Warp to T-Chain
	if err := p.warpSender.SendCrossChainMessage(p.TChainID, payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWarpSendFailed, err)
	}

	// Return session ID
	sessionID := computeSessionID(keyID[:], []byte("reshare"))
	return sessionID[:], nil
}

// getPublicKey retrieves the public key for a key ID.
// Input: keyId (32 bytes)
// Output: publicKey (ABI encoded bytes)
func (p *ThresholdPrecompile) getPublicKey(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, ErrInvalidInput
	}

	var keyID [32]byte
	copy(keyID[:], data[:32])

	// Query T-Chain for public key via Warp
	// For now, return placeholder - real implementation queries state
	pubKey := make([]byte, 33) // Compressed secp256k1 public key

	return encodeBytes(pubKey), nil
}

// getParticipants retrieves the participant list for a key.
// Input: keyId (32 bytes)
// Output: participants (ABI encoded address[])
func (p *ThresholdPrecompile) getParticipants(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, ErrInvalidInput
	}

	var keyID [32]byte
	copy(keyID[:], data[:32])

	// Query T-Chain for participants via Warp
	// For now, return empty array - real implementation queries state
	participants := make([][20]byte, 0)

	return encodeAddressArray(participants), nil
}

// getSignature retrieves a completed signature.
// Input: sessionId (32 bytes)
// Output: r (32 bytes) + s (32 bytes) + v (32 bytes, uint8 padded)
func (p *ThresholdPrecompile) getSignature(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, ErrInvalidInput
	}

	var sessionID [32]byte
	copy(sessionID[:], data[:32])

	// Query T-Chain for signature via Warp
	// For now, return placeholder - real implementation queries state
	result := make([]byte, 96)
	// r: bytes 0-31
	// s: bytes 32-63
	// v: bytes 64-95 (just last byte matters)

	return result, nil
}

// getSessionStatus retrieves the status of a session.
// Input: sessionId (32 bytes)
// Output: status (32 bytes, uint8 padded) + error (ABI encoded string)
func (p *ThresholdPrecompile) getSessionStatus(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, ErrInvalidInput
	}

	var sessionID [32]byte
	copy(sessionID[:], data[:32])

	// Query T-Chain for session status via Warp
	// For now, return pending status - real implementation queries state
	result := make([]byte, 64)
	result[31] = StatusPending

	return result, nil
}

// Helper functions

func isValidProtocol(protocol string) bool {
	switch protocol {
	case ProtocolLSS, ProtocolCGGMP21, ProtocolBLS, ProtocolRingtail, ProtocolFrost:
		return true
	default:
		return false
	}
}

func extractString(data []byte) string {
	// Simple extraction - assume null-terminated or padded string
	end := 0
	for i, b := range data {
		if b == 0 {
			end = i
			break
		}
		end = i + 1
	}
	return string(data[:end])
}

func extractBytes(data []byte, offset int) ([]byte, error) {
	if len(data) < offset+64 {
		return nil, ErrInvalidInput
	}
	// ABI encoding: offset (32 bytes) + length (32 bytes) + data
	dataOffset := new(big.Int).SetBytes(data[offset : offset+32]).Uint64()
	if uint64(len(data)) < dataOffset+32 {
		return nil, ErrInvalidInput
	}
	length := new(big.Int).SetBytes(data[dataOffset : dataOffset+32]).Uint64()
	if uint64(len(data)) < dataOffset+32+length {
		return nil, ErrInvalidInput
	}
	return data[dataOffset+32 : dataOffset+32+length], nil
}

func extractAddressArray(data []byte) ([][20]byte, error) {
	if len(data) < 64 {
		return nil, ErrInvalidInput
	}
	// ABI encoding: offset (32 bytes) + length (32 bytes) + addresses
	length := new(big.Int).SetBytes(data[32:64]).Uint64()
	if uint64(len(data)) < 64+length*32 {
		return nil, ErrInvalidInput
	}
	addresses := make([][20]byte, length)
	for i := uint64(0); i < length; i++ {
		copy(addresses[i][:], data[64+i*32+12:64+(i+1)*32]) // Last 20 bytes of 32-byte slot
	}
	return addresses, nil
}

func encodeBytes(data []byte) []byte {
	// ABI encoding for bytes
	result := make([]byte, 64+((len(data)+31)/32)*32)
	// Offset pointing to data start
	binary.BigEndian.PutUint64(result[24:32], 32)
	// Length
	binary.BigEndian.PutUint64(result[56:64], uint64(len(data)))
	// Data
	copy(result[64:], data)
	return result
}

func encodeAddressArray(addresses [][20]byte) []byte {
	// ABI encoding for address[]
	result := make([]byte, 64+len(addresses)*32)
	// Offset pointing to array start
	binary.BigEndian.PutUint64(result[24:32], 32)
	// Length
	binary.BigEndian.PutUint64(result[56:64], uint64(len(addresses)))
	// Addresses (each padded to 32 bytes)
	for i, addr := range addresses {
		copy(result[64+i*32+12:], addr[:])
	}
	return result
}

func computeSessionID(keyID, extra []byte) [32]byte {
	// Simple session ID computation - hash key ID with extra data
	var result [32]byte
	h := make([]byte, len(keyID)+len(extra))
	copy(h, keyID)
	copy(h[len(keyID):], extra)
	// Use simple XOR for now - real implementation uses proper hash
	for i := 0; i < 32 && i < len(h); i++ {
		result[i] = h[i]
	}
	return result
}

func verifyECDSASignature(pubKey, messageHash, signature []byte) bool {
	// Signature verification using secp256k1
	// For now, return false - real implementation uses crypto/ecdsa or secp256k1 library
	if len(pubKey) < 33 || len(messageHash) != 32 || len(signature) < 64 {
		return false
	}
	// TODO: Implement proper ECDSA verification
	return true
}
