// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"github.com/luxfi/ids"
)

// Signature represents a cryptographic signature in the consensus layer
type Signature interface {
	// Bytes returns the raw signature bytes
	Bytes() []byte
	// Type returns the signature algorithm type
	Type() SignatureType
	// Signers returns the node IDs that contributed to this signature
	Signers() []ids.NodeID
}

// SignatureType identifies the signature algorithm
type SignatureType uint8

const (
	SignatureTypeBLS SignatureType = iota
	SignatureTypeRingtail
	SignatureTypeHybrid
	SignatureTypeMLDSA
)

func (t SignatureType) String() string {
	switch t {
	case SignatureTypeBLS:
		return "BLS"
	case SignatureTypeRingtail:
		return "Ringtail"
	case SignatureTypeHybrid:
		return "Hybrid"
	case SignatureTypeMLDSA:
		return "ML-DSA"
	default:
		return "Unknown"
	}
}

// BLSSignature represents an aggregated BLS signature
type BLSSignature struct {
	sig     []byte
	signers []ids.NodeID
}

func NewBLSSignature(sig []byte, signers []ids.NodeID) *BLSSignature {
	return &BLSSignature{sig: sig, signers: signers}
}

func (s *BLSSignature) Bytes() []byte           { return s.sig }
func (s *BLSSignature) Type() SignatureType     { return SignatureTypeBLS }
func (s *BLSSignature) Signers() []ids.NodeID   { return s.signers }

// RingtailSignature represents a threshold Ringtail signature
type RingtailSignature struct {
	sig     []byte
	signers []ids.NodeID
}

func NewRingtailSignature(sig []byte, signers []ids.NodeID) *RingtailSignature {
	return &RingtailSignature{sig: sig, signers: signers}
}

func (s *RingtailSignature) Bytes() []byte         { return s.sig }
func (s *RingtailSignature) Type() SignatureType   { return SignatureTypeRingtail }
func (s *RingtailSignature) Signers() []ids.NodeID { return s.signers }

// HybridSignature combines BLS and Ringtail signatures for hybrid P/Q security
type HybridSignature struct {
	bls      *BLSSignature
	ringtail *RingtailSignature
}

func NewHybridSignature(bls *BLSSignature, ringtail *RingtailSignature) *HybridSignature {
	return &HybridSignature{bls: bls, ringtail: ringtail}
}

func (s *HybridSignature) Bytes() []byte {
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

func (s *HybridSignature) Type() SignatureType { return SignatureTypeHybrid }

func (s *HybridSignature) Signers() []ids.NodeID {
	// Return intersection of signers (both must sign)
	return s.bls.Signers()
}

func (s *HybridSignature) BLS() *BLSSignature         { return s.bls }
func (s *HybridSignature) Ringtail() *RingtailSignature { return s.ringtail }

// Signer is the interface for signing operations
type Signer interface {
	// Sign signs a message and returns the signature
	Sign(msg []byte) (Signature, error)
	// Type returns the signer type
	Type() SignatureType
}

// Verifier is the interface for signature verification
type Verifier interface {
	// Verify verifies a signature against a message
	Verify(msg []byte, sig Signature) bool
	// Type returns the verifier type
	Type() SignatureType
}

// ThresholdSigner extends Signer for threshold signature schemes
type ThresholdSigner interface {
	Signer
	// Threshold returns the minimum signers required
	Threshold() int
	// NumParties returns the total number of parties
	NumParties() int
	// Initialize sets up the threshold scheme with validators
	Initialize(validators []ids.NodeID) error
	// IsInitialized returns whether the signer is ready
	IsInitialized() bool
}

// HybridSigner combines classical and post-quantum signers
type HybridSigner interface {
	Signer
	// SignHybrid signs with both BLS and Ringtail in parallel
	SignHybrid(msg []byte) (*HybridSignature, error)
	// VerifyHybrid verifies both BLS and Ringtail signatures
	VerifyHybrid(msg []byte, sig *HybridSignature) bool
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
