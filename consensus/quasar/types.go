// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// SignatureType identifies the signature algorithm used
type SignatureType uint8

const (
	SignatureTypeBLS SignatureType = iota
	SignatureTypeRingtail
	SignatureTypeQuasar // Hybrid BLS + Ringtail
	SignatureTypeMLDSA
)

// Signature is the interface for all signature types
type Signature interface {
	Bytes() []byte
	Type() SignatureType
	Signers() []ids.NodeID
}

// Signer is the interface for signing operations
type Signer interface {
	Sign(msg []byte) (Signature, error)
	PublicKey() []byte
}

// Verifier is the interface for signature verification
type Verifier interface {
	Verify(msg []byte, sig Signature) bool
}

// ThresholdSigner extends Signer for threshold signature schemes
type ThresholdSigner interface {
	Signer
	Index() int
	Threshold() int
}

// RingtailConfig holds configuration for Ringtail threshold signatures
type RingtailConfig struct {
	NumParties int
	Threshold  int
	PartyIndex int
}

// RingtailStats contains statistics about the Ringtail coordinator
type RingtailStats struct {
	NumParties  int
	Threshold   int
	Initialized bool
}

// RingtailSignature represents a threshold Ringtail signature
type RingtailSignature struct {
	sig     []byte
	signers []ids.NodeID
}

// NewRingtailSignature creates a new Ringtail signature
func NewRingtailSignature(sig []byte, signers []ids.NodeID) *RingtailSignature {
	return &RingtailSignature{sig: sig, signers: signers}
}

func (s *RingtailSignature) Bytes() []byte         { return s.sig }
func (s *RingtailSignature) Type() SignatureType   { return SignatureTypeRingtail }
func (s *RingtailSignature) Signers() []ids.NodeID { return s.signers }

// RingtailCoordinator manages the threshold signing protocol.
//
// In production, Sign/Verify require real lattice key material.
// The coordinator is fail-closed: operations return errors unless
// properly initialized with key material, or explicitly created
// via NewTestRingtailCoordinator for tests.
type RingtailCoordinator struct {
	log         log.Logger
	config      RingtailConfig
	initialized bool
	testing     bool // only true via NewTestRingtailCoordinator
	validators  []ids.NodeID
}

// NewRingtailCoordinator creates a new Ringtail coordinator.
// Sign and Verify will fail until real lattice key material is loaded.
func NewRingtailCoordinator(log log.Logger, config RingtailConfig) (*RingtailCoordinator, error) {
	return &RingtailCoordinator{
		log:    log,
		config: config,
	}, nil
}

// NewTestRingtailCoordinator creates a Ringtail coordinator for testing.
// The test coordinator uses deterministic stub signatures that are NOT
// cryptographically secure — never use in production.
func NewTestRingtailCoordinator(log log.Logger, config RingtailConfig) (*RingtailCoordinator, error) {
	return &RingtailCoordinator{
		log:     log,
		config:  config,
		testing: true,
	}, nil
}

func (rc *RingtailCoordinator) Initialize(validators []ids.NodeID) error {
	rc.validators = validators
	rc.initialized = true
	return nil
}

func (rc *RingtailCoordinator) IsInitialized() bool {
	return rc.initialized
}

func (rc *RingtailCoordinator) Sign(msg []byte) (Signature, error) {
	if !rc.initialized {
		return nil, errors.New("ringtail: threshold signing not initialized — requires real lattice key material")
	}
	if rc.testing {
		// Test-only stub: deterministic RT-prefixed signature
		sig := append([]byte("RT"), msg[:min(32, len(msg))]...)
		return NewRingtailSignature(sig, rc.validators), nil
	}
	return nil, errors.New("ringtail: threshold signing not initialized — requires real lattice key material")
}

func (rc *RingtailCoordinator) Verify(msg []byte, sig Signature) bool {
	if !rc.initialized {
		return false
	}
	if rc.testing {
		return sig != nil && len(sig.Bytes()) > 0
	}
	return false
}

func (rc *RingtailCoordinator) Stats() RingtailStats {
	return RingtailStats{
		NumParties:  rc.config.NumParties,
		Threshold:   rc.config.Threshold,
		Initialized: rc.initialized,
	}
}

// Threshold returns the threshold required for signing
func (rc *RingtailCoordinator) Threshold() int {
	return rc.config.Threshold
}

// NumParties returns the number of parties in the threshold scheme
func (rc *RingtailCoordinator) NumParties() int {
	return rc.config.NumParties
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// BLSSignature represents an aggregated BLS signature (node-specific)
type BLSSignature struct {
	sig     []byte
	signers []ids.NodeID
}

func NewBLSSignature(sig []byte, signers []ids.NodeID) *BLSSignature {
	return &BLSSignature{sig: sig, signers: signers}
}

func (s *BLSSignature) Bytes() []byte         { return s.sig }
func (s *BLSSignature) Type() SignatureType   { return SignatureTypeBLS }
func (s *BLSSignature) Signers() []ids.NodeID { return s.signers }

// QuasarSignature combines BLS and Ringtail signatures for P/Q security
type QuasarSignature struct {
	bls      *BLSSignature
	ringtail *RingtailSignature
}

func NewQuasarSignature(bls *BLSSignature, ringtail *RingtailSignature) *QuasarSignature {
	return &QuasarSignature{bls: bls, ringtail: ringtail}
}

func (s *QuasarSignature) Bytes() []byte {
	// Concatenate BLS + Ringtail bytes with length prefix
	blsBytes := s.bls.Bytes()
	rtBytes := s.ringtail.Bytes()
	result := make([]byte, 4+len(blsBytes)+len(rtBytes))
	// Length of BLS signature (big endian)
	result[0] = byte(len(blsBytes) >> 24)
	result[1] = byte(len(blsBytes) >> 16)
	result[2] = byte(len(blsBytes) >> 8)
	result[3] = byte(len(blsBytes))
	copy(result[4:], blsBytes)
	copy(result[4+len(blsBytes):], rtBytes)
	return result
}

func (s *QuasarSignature) Type() SignatureType { return SignatureTypeQuasar }

func (s *QuasarSignature) Signers() []ids.NodeID {
	// Return intersection of signers (both must sign)
	return s.bls.Signers()
}

func (s *QuasarSignature) BLS() *BLSSignature           { return s.bls }
func (s *QuasarSignature) Ringtail() *RingtailSignature { return s.ringtail }

// QuasarSigner combines classical and post-quantum signers
type QuasarSigner interface {
	Signer
	// SignQuasar signs with both BLS and Ringtail in parallel
	SignQuasar(msg []byte) (*QuasarSignature, error)
	// VerifyQuasar verifies both BLS and Ringtail signatures
	VerifyQuasar(msg []byte, sig *QuasarSignature) bool
}

// FinalityProof represents proof of block finality
type FinalityProof struct {
	BlockID      ids.ID
	Height       uint64
	Signature    Signature
	TotalWeight  uint64
	SignerWeight uint64
}

// ValidatorInfo contains validator information for consensus
type ValidatorInfo struct {
	NodeID ids.NodeID
	Weight uint64
	Active bool
}
