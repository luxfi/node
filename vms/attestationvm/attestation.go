// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package attestationvm

import (
	"crypto/sha256"
	"encoding/binary"
	"time"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/crypto/aggregated"
)

// AttestationType represents the type of attestation
type AttestationType uint8

const (
	AttestationTypeOracle AttestationType = iota
	AttestationTypeTEE
	AttestationTypeGPU
	AttestationTypeCustom
)

// Attestation represents a signed attestation
type Attestation struct {
	ID          ids.ID          `json:"id"`
	Type        AttestationType `json:"type"`
	SourceID    string          `json:"sourceId"`    // Oracle ID, TEE ID, etc.
	Data        []byte          `json:"data"`        // The attested data
	Timestamp   int64           `json:"timestamp"`
	Signatures  [][]byte        `json:"signatures"`  // Threshold signatures
	SignerIDs   []string        `json:"signerIds"`   // IDs of signers
	Proof       []byte          `json:"proof"`       // ZK proof or TEE quote
	Metadata    []byte          `json:"metadata"`    // Additional metadata
	
	// Aggregated signature support
	AggregatedSignature *aggregated.AggregatedSignature `json:"aggregatedSignature,omitempty"`
}

// ComputeID computes the ID of an attestation
func (a *Attestation) ComputeID() ids.ID {
	h := sha256.New()
	h.Write([]byte{byte(a.Type)})
	h.Write([]byte(a.SourceID))
	h.Write(a.Data)
	binary.Write(h, binary.BigEndian, a.Timestamp)
	return ids.ID(h.Sum(nil))
}

// AttestationRequest represents a request for attestation
type AttestationRequest struct {
	RequestID   ids.ID          `json:"requestId"`
	Type        AttestationType `json:"type"`
	DataToSign  []byte          `json:"dataToSign"`
	RequiredSigs int            `json:"requiredSigs"`
	Deadline    time.Time       `json:"deadline"`
}

// AttestationResult represents the result of an attestation
type AttestationResult struct {
	RequestID    ids.ID      `json:"requestId"`
	Attestation  *Attestation `json:"attestation"`
	Success      bool        `json:"success"`
	Error        string      `json:"error,omitempty"`
}

// OracleInfo represents oracle information
type OracleInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	PublicKey   []byte   `json:"publicKey"`
	Endpoint    string   `json:"endpoint"`
	Feeds       []string `json:"feeds"`      // Types of data this oracle provides
	Reputation  uint64   `json:"reputation"`
	JoinedAt    int64    `json:"joinedAt"`
}

// TEEInfo represents TEE information
type TEEInfo struct {
	ID             string `json:"id"`
	EnclaveID      string `json:"enclaveId"`
	MeasurementHash []byte `json:"measurementHash"`
	PublicKey      []byte `json:"publicKey"`
	Platform       string `json:"platform"` // SGX, TrustZone, etc.
	Version        string `json:"version"`
}

// GPUProofInfo represents GPU computation proof info
type GPUProofInfo struct {
	ID           string `json:"id"`
	ComputeType  string `json:"computeType"`  // FFT, ML inference, etc.
	ProofSystem  string `json:"proofSystem"`  // Groth16, PLONK, etc.
	VerifyingKey []byte `json:"verifyingKey"`
	CircuitHash  []byte `json:"circuitHash"`
}