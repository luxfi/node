// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
)

// WarpHandler handles incoming Warp messages from T-Chain and routes them
// to the appropriate callback contracts.
type WarpHandler struct {
	mu sync.RWMutex

	// Pending requests waiting for T-Chain response
	pendingRequests map[[32]byte]*PendingRequest

	// Completed signatures ready for retrieval
	completedSigs map[[32]byte]*CompletedSignature

	// Key information cache
	keys map[[32]byte]*KeyInfo

	// Configuration
	requestTimeout time.Duration
	maxPending     int
}

// PendingRequest tracks a request waiting for T-Chain response.
type PendingRequest struct {
	RequestID    [32]byte
	RequestType  uint8
	KeyID        [32]byte
	CreatedAt    time.Time
	ExpiresAt    time.Time
	Requester    [20]byte
	Callback     [20]byte
	CallbackData []byte
	Status       uint8
}

// CompletedSignature holds a signature result from T-Chain.
type CompletedSignature struct {
	RequestID    [32]byte
	KeyID        [32]byte
	R            [32]byte
	S            [32]byte
	V            uint8
	MessageHash  [32]byte
	CompletedAt  time.Time
	ValidatorSig [32]byte // Aggregated BLS signature from T-Chain validators
}

// KeyInfo caches threshold key information.
type KeyInfo struct {
	KeyID        [32]byte
	Protocol     string
	PublicKey    []byte
	Threshold    uint8
	TotalParties uint8
	Participants [][20]byte
	Generation   uint64
	CreatedAt    time.Time
	LastUsedAt   time.Time
}

// NewWarpHandler creates a new Warp message handler.
func NewWarpHandler() *WarpHandler {
	return &WarpHandler{
		pendingRequests: make(map[[32]byte]*PendingRequest),
		completedSigs:   make(map[[32]byte]*CompletedSignature),
		keys:            make(map[[32]byte]*KeyInfo),
		requestTimeout:  5 * time.Minute,
		maxPending:      1000,
	}
}

// HandleWarpMessage processes an incoming Warp message from T-Chain.
func (h *WarpHandler) HandleWarpMessage(ctx context.Context, sourceChainID ids.ID, payload []byte) error {
	if len(payload) < 2 {
		return ErrPayloadTooShort
	}

	version := payload[0]
	if version != PayloadVersionV1 {
		return ErrInvalidPayloadVersion
	}

	payloadType := payload[1]

	switch payloadType {
	case PayloadTypeKeygenResult:
		return h.handleKeygenResult(payload)
	case PayloadTypeSignResult:
		return h.handleSignResult(payload)
	case PayloadTypeRefreshResult:
		return h.handleRefreshResult(payload)
	case PayloadTypeReshareResult:
		return h.handleReshareResult(payload)
	case PayloadTypeQueryResult:
		return h.handleQueryResult(payload)
	default:
		return ErrInvalidPayloadType
	}
}

// handleKeygenResult processes a DKG completion result from T-Chain.
func (h *WarpHandler) handleKeygenResult(payload []byte) error {
	// Parse keygen result payload
	// Expected format: version(1) + type(1) + requestId(32) + status(1) + publicKey(variable)
	if len(payload) < 66 {
		return ErrPayloadTooShort
	}

	var requestID [32]byte
	copy(requestID[:], payload[2:34])
	status := payload[34]

	h.mu.Lock()
	defer h.mu.Unlock()

	pending, ok := h.pendingRequests[requestID]
	if !ok {
		return errors.New("no pending request found")
	}

	if status == ResultStatusSuccess {
		// Extract public key and create KeyInfo
		pubKeyLen := int(payload[35])
		if len(payload) < 36+pubKeyLen {
			return ErrPayloadTooShort
		}
		pubKey := payload[36 : 36+pubKeyLen]

		keyInfo := &KeyInfo{
			KeyID:      pending.KeyID,
			PublicKey:  pubKey,
			CreatedAt:  time.Now(),
			LastUsedAt: time.Now(),
		}
		h.keys[pending.KeyID] = keyInfo
	}

	pending.Status = status
	delete(h.pendingRequests, requestID)

	return nil
}

// handleSignResult processes a signing result from T-Chain.
func (h *WarpHandler) handleSignResult(payload []byte) error {
	result, err := ParseSignResultPayload(payload)
	if err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	pending, ok := h.pendingRequests[result.RequestID]
	if !ok {
		return errors.New("no pending request found")
	}

	if result.Status == ResultStatusSuccess {
		completed := &CompletedSignature{
			RequestID:    result.RequestID,
			KeyID:        pending.KeyID,
			R:            result.R,
			S:            result.S,
			V:            result.V,
			CompletedAt:  time.Now(),
			ValidatorSig: result.CommitteeSignature,
		}
		h.completedSigs[result.RequestID] = completed

		// Update key last used time
		if keyInfo, ok := h.keys[pending.KeyID]; ok {
			keyInfo.LastUsedAt = time.Now()
		}
	}

	pending.Status = result.Status
	delete(h.pendingRequests, result.RequestID)

	return nil
}

// handleRefreshResult processes a key refresh result from T-Chain.
func (h *WarpHandler) handleRefreshResult(payload []byte) error {
	if len(payload) < 35 {
		return ErrPayloadTooShort
	}

	var requestID [32]byte
	copy(requestID[:], payload[2:34])
	status := payload[34]

	h.mu.Lock()
	defer h.mu.Unlock()

	pending, ok := h.pendingRequests[requestID]
	if !ok {
		return errors.New("no pending request found")
	}

	if status == ResultStatusSuccess {
		// Update key generation
		if keyInfo, ok := h.keys[pending.KeyID]; ok {
			keyInfo.Generation++
			keyInfo.LastUsedAt = time.Now()
		}
	}

	pending.Status = status
	delete(h.pendingRequests, requestID)

	return nil
}

// handleReshareResult processes a key reshare result from T-Chain.
func (h *WarpHandler) handleReshareResult(payload []byte) error {
	if len(payload) < 35 {
		return ErrPayloadTooShort
	}

	var requestID [32]byte
	copy(requestID[:], payload[2:34])
	status := payload[34]

	h.mu.Lock()
	defer h.mu.Unlock()

	pending, ok := h.pendingRequests[requestID]
	if !ok {
		return errors.New("no pending request found")
	}

	pending.Status = status
	delete(h.pendingRequests, requestID)

	return nil
}

// handleQueryResult processes a query result from T-Chain.
func (h *WarpHandler) handleQueryResult(payload []byte) error {
	// Query results update local cache
	return nil
}

// AddPendingRequest adds a new pending request.
func (h *WarpHandler) AddPendingRequest(req *PendingRequest) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.pendingRequests) >= h.maxPending {
		return errors.New("max pending requests reached")
	}

	h.pendingRequests[req.RequestID] = req
	return nil
}

// GetSignature retrieves a completed signature.
func (h *WarpHandler) GetSignature(requestID [32]byte) (*CompletedSignature, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	sig, ok := h.completedSigs[requestID]
	if !ok {
		// Check if still pending
		if pending, ok := h.pendingRequests[requestID]; ok {
			if pending.Status == StatusPending || pending.Status == StatusRunning {
				return nil, errors.New("signature pending")
			}
			return nil, errors.New("signature failed")
		}
		return nil, ErrSessionNotFound
	}

	return sig, nil
}

// GetRequestStatus returns the status of a request.
func (h *WarpHandler) GetRequestStatus(requestID [32]byte) (uint8, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if pending, ok := h.pendingRequests[requestID]; ok {
		return pending.Status, nil
	}

	if _, ok := h.completedSigs[requestID]; ok {
		return StatusCompleted, nil
	}

	return 0, ErrSessionNotFound
}

// GetKeyInfo retrieves cached key information.
func (h *WarpHandler) GetKeyInfo(keyID [32]byte) (*KeyInfo, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	keyInfo, ok := h.keys[keyID]
	if !ok {
		return nil, ErrKeyNotFound
	}

	return keyInfo, nil
}

// CleanupExpired removes expired pending requests.
func (h *WarpHandler) CleanupExpired() {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	for requestID, pending := range h.pendingRequests {
		if now.After(pending.ExpiresAt) {
			pending.Status = StatusExpired
			delete(h.pendingRequests, requestID)
		}
	}

	// Also cleanup old completed signatures (keep for 1 hour)
	for requestID, sig := range h.completedSigs {
		if now.Sub(sig.CompletedAt) > time.Hour {
			delete(h.completedSigs, requestID)
		}
	}
}

// VerifySignature verifies an ECDSA signature using secp256k1.
func VerifySignature(pubKey []byte, messageHash []byte, r, s *big.Int) bool {
	if len(pubKey) == 0 || len(messageHash) != 32 {
		return false
	}

	// Decompress public key if compressed
	var x, y *big.Int
	if len(pubKey) == 33 {
		x, y = secp256k1.DecompressPubkey(pubKey)
		if x == nil || y == nil {
			return false
		}
	} else if len(pubKey) == 65 {
		x = new(big.Int).SetBytes(pubKey[1:33])
		y = new(big.Int).SetBytes(pubKey[33:65])
	} else {
		return false
	}

	// Create public key structure
	ecPubKey := &ecdsa.PublicKey{
		Curve: secp256k1.S256(),
		X:     x,
		Y:     y,
	}

	// Verify signature
	return ecdsa.Verify(ecPubKey, messageHash, r, s)
}

// PublicKeyToAddress converts a secp256k1 public key to an Ethereum address.
func PublicKeyToAddress(pubKey []byte) [20]byte {
	var addr [20]byte

	// Decompress if needed
	var uncompressed []byte
	if len(pubKey) == 33 {
		x, y := secp256k1.DecompressPubkey(pubKey)
		if x == nil || y == nil {
			return addr
		}
		uncompressed = make([]byte, 64)
		xBytes := x.Bytes()
		yBytes := y.Bytes()
		copy(uncompressed[32-len(xBytes):32], xBytes)
		copy(uncompressed[64-len(yBytes):64], yBytes)
	} else if len(pubKey) == 65 {
		uncompressed = pubKey[1:]
	} else if len(pubKey) == 64 {
		uncompressed = pubKey
	} else {
		return addr
	}

	// Hash with Keccak256 and take last 20 bytes
	// For now, use SHA256 as placeholder (real impl uses Keccak)
	hash := sha256.Sum256(uncompressed)
	copy(addr[:], hash[12:32])

	return addr
}

// RequestIDFromKeyAndMessage generates a deterministic request ID.
func RequestIDFromKeyAndMessage(keyID [32]byte, messageHash [32]byte) [32]byte {
	var requestID [32]byte
	h := sha256.New()
	h.Write(keyID[:])
	h.Write(messageHash[:])
	copy(requestID[:], h.Sum(nil))
	return requestID
}

// FormatSignature formats r, s, v into a 65-byte signature.
func FormatSignature(r, s [32]byte, v uint8) []byte {
	sig := make([]byte, 65)
	copy(sig[0:32], r[:])
	copy(sig[32:64], s[:])
	sig[64] = v
	return sig
}

// ParseSignature parses a 65-byte signature into r, s, v.
func ParseSignature(sig []byte) (r [32]byte, s [32]byte, v uint8, err error) {
	if len(sig) != 65 {
		err = fmt.Errorf("invalid signature length: %d", len(sig))
		return
	}
	copy(r[:], sig[0:32])
	copy(s[:], sig[32:64])
	v = sig[64]
	return
}

// HexToBytes32 converts a hex string to a 32-byte array.
func HexToBytes32(hexStr string) ([32]byte, error) {
	var result [32]byte

	// Remove 0x prefix if present
	if len(hexStr) >= 2 && hexStr[:2] == "0x" {
		hexStr = hexStr[2:]
	}

	bytes, err := hex.DecodeString(hexStr)
	if err != nil {
		return result, err
	}

	if len(bytes) != 32 {
		return result, fmt.Errorf("invalid length: expected 32, got %d", len(bytes))
	}

	copy(result[:], bytes)
	return result, nil
}
