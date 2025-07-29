// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package beam

import (
	"errors"
	
	rt "github.com/luxfi/ringtail"
)

var (
	ErrQuasarTimeout = errors.New("quasar certificate timeout")
	ErrBLS          = errors.New("BLS signature verification failed")
	ErrRingtail     = errors.New("Ringtail signature verification failed")
)

// CertBundle contains both BLS and Ringtail certificates for dual finality
type CertBundle struct {
	BLSAgg [96]byte // BLS aggregate signature
	RTCert []byte   // Ringtail certificate (nil until aggregated)
}

// Block represents a Beam consensus block with dual certificates
type Block struct {
	Header
	Certs CertBundle
	Txs   [][]byte
}

// Header contains block metadata
type Header struct {
	Height    uint64
	ParentID  [32]byte
	Timestamp int64
	TxRoot    [32]byte
}

// Hash computes the block hash
func (b *Block) Hash() [32]byte {
	// In production, this would use proper serialization
	// For now, simplified hashing
	data := make([]byte, 0)
	data = append(data, b.Header.ParentID[:]...)
	// Add other fields...
	
	var hash [32]byte
	copy(hash[:], data[:32])
	return hash
}

// VerifyBlock performs fast-path verification with no extra locks
func VerifyBlock(b *Block, pkGroup []byte, q *quasarState) error {
	msg := b.Hash()
	
	// Verify BLS aggregate signature
	if !verifyBLS(b.Certs.BLSAgg, msg[:]) {
		return ErrBLS
	}
	
	// Verify Ringtail certificate
	if !rt.Verify(pkGroup, msg[:], b.Certs.RTCert) {
		return ErrRingtail
	}
	
	return nil
}

// verifyBLS checks the BLS aggregate signature
func verifyBLS(sig [96]byte, msg []byte) bool {
	// In production, use actual BLS verification
	// For now, basic check
	return len(sig) == 96 && len(msg) > 0
}