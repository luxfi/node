// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainadapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
)

// Confidential compute errors
var (
	ErrTEENotAvailable      = errors.New("TEE not available")
	ErrAttestationFailed    = errors.New("attestation verification failed")
	ErrComputeTimeout       = errors.New("compute operation timed out")
	ErrInvalidQuote         = errors.New("invalid TEE quote")
	ErrCommitteeCertInvalid = errors.New("committee certificate invalid")
)

// TEEType defines the type of Trusted Execution Environment
type TEEType uint8

const (
	// TEEIntelSGX is Intel SGX
	TEEIntelSGX TEEType = iota
	// TEEAMDSev is AMD SEV
	TEEAMDSev
	// TEENvidiaCC is NVIDIA Confidential Computing
	TEENvidiaCC
	// TEEArmTrustZone is ARM TrustZone
	TEEArmTrustZone
	// TEEAWSNitro is AWS Nitro Enclaves
	TEEAWSNitro
	// TEEAzureSGX is Azure Confidential Computing (SGX)
	TEEAzureSGX
)

// String returns the string representation of TEEType
func (t TEEType) String() string {
	switch t {
	case TEEIntelSGX:
		return "intel_sgx"
	case TEEAMDSev:
		return "amd_sev"
	case TEENvidiaCC:
		return "nvidia_cc"
	case TEEArmTrustZone:
		return "arm_trustzone"
	case TEEAWSNitro:
		return "aws_nitro"
	case TEEAzureSGX:
		return "azure_sgx"
	default:
		return "unknown"
	}
}

// ComputeMode defines how confidential compute is performed
type ComputeMode uint8

const (
	// ComputeModeTEE uses Trusted Execution Environment
	ComputeModeTEE ComputeMode = iota
	// ComputeModeFHE uses Fully Homomorphic Encryption
	ComputeModeFHE
	// ComputeModeZK uses Zero-Knowledge Proofs
	ComputeModeZK
	// ComputeModeHybrid combines TEE + FHE or TEE + ZK
	ComputeModeHybrid
)

// ComputeRequest represents a request for confidential computation
type ComputeRequest struct {
	ID            ids.ID       `json:"id"`
	AppChainID    ids.ID       `json:"appChainId"`
	Requester     []byte       `json:"requester"`

	// Input specification
	InputRefs     []DataRef    `json:"inputRefs"`     // References to encrypted input data
	InputCommitment [32]byte   `json:"inputCommitment"`

	// Computation specification
	ComputeMode   ComputeMode  `json:"computeMode"`
	Program       []byte       `json:"program"`       // WASM, bytecode, or program hash
	ProgramHash   [32]byte     `json:"programHash"`
	Parameters    []byte       `json:"parameters"`

	// Output specification
	OutputDomain  EncryptionDomain `json:"outputDomain"`
	OutputRecipients [][]byte      `json:"outputRecipients"`

	// Attestation requirements
	RequireTEE    bool         `json:"requireTee"`
	AcceptedTEEs  []TEEType    `json:"acceptedTees"`
	RequireProof  bool         `json:"requireProof"`

	// Timing
	Deadline      time.Time    `json:"deadline"`
	MaxGas        uint64       `json:"maxGas"`
}

// DataRef references encrypted data for compute
type DataRef struct {
	DocumentID    DocumentID   `json:"documentId"`
	DAPointer     *DAPointer   `json:"daPointer"`
	Commitment    [32]byte     `json:"commitment"`
	Domain        EncryptionDomain `json:"domain"`
}

// ComputeResult represents the result of confidential computation
type ComputeResult struct {
	RequestID       ids.ID       `json:"requestId"`
	Status          ComputeStatus `json:"status"`

	// Output
	OutputData      []byte       `json:"outputData"`      // Encrypted
	OutputCommitment [32]byte    `json:"outputCommitment"`
	OutputDAPointer *DAPointer   `json:"outputDaPointer,omitempty"`

	// Attestation
	Attestation    *TEEAttestation `json:"attestation,omitempty"`
	Proof          *ComputeProof   `json:"proof,omitempty"`
	CommitteeCert  *CommitteeCert  `json:"committeeCert,omitempty"`

	// Timing
	ComputedAt     time.Time    `json:"computedAt"`
	GasUsed        uint64       `json:"gasUsed"`
}

// ComputeStatus represents the status of a compute request
type ComputeStatus uint8

const (
	ComputeStatusPending ComputeStatus = iota
	ComputeStatusRunning
	ComputeStatusCompleted
	ComputeStatusFailed
	ComputeStatusTimeout
)

// TEEAttestation contains attestation evidence from a TEE
type TEEAttestation struct {
	TEEType       TEEType      `json:"teeType"`
	Quote         []byte       `json:"quote"`          // Platform-specific quote
	QuoteVersion  uint32       `json:"quoteVersion"`

	// Measurements
	MRENCLAVE     [32]byte     `json:"mrenclave,omitempty"` // SGX enclave measurement
	MRSIGNER      [32]byte     `json:"mrsigner,omitempty"`  // SGX signer measurement
	ProductID     uint16       `json:"productId,omitempty"`
	SecurityVersion uint16     `json:"securityVersion,omitempty"`

	// Report data (binds to computation)
	ReportData    [64]byte     `json:"reportData"`

	// Input/Output commitments in report
	InputCommitment  [32]byte  `json:"inputCommitment"`
	OutputCommitment [32]byte  `json:"outputCommitment"`
	ProgramHash      [32]byte  `json:"programHash"`

	// Certification chain
	CertChain     [][]byte     `json:"certChain"`

	// Timestamp
	Timestamp     time.Time    `json:"timestamp"`
	Expiry        time.Time    `json:"expiry"`
}

// Verify verifies the TEE attestation
func (a *TEEAttestation) Verify() error {
	// In production, verify:
	// 1. Quote signature using Intel/AMD/NVIDIA attestation service
	// 2. Certificate chain validity
	// 3. Security version is acceptable
	// 4. Report data matches expected commitments

	// Verify report data contains our commitments
	expectedReportData := computeReportData(a.InputCommitment, a.OutputCommitment, a.ProgramHash)
	if a.ReportData != expectedReportData {
		return ErrInvalidQuote
	}

	return nil
}

// computeReportData computes expected report data from commitments
func computeReportData(input, output, program [32]byte) [64]byte {
	h := sha256.New()
	h.Write(input[:])
	h.Write(output[:])
	h.Write(program[:])
	hash := h.Sum(nil)

	var reportData [64]byte
	copy(reportData[:32], hash)
	return reportData
}

// ComputeProof represents a ZK proof of correct computation
type ComputeProof struct {
	ProofSystem   string       `json:"proofSystem"`   // "groth16", "plonk", "stark"
	Proof         []byte       `json:"proof"`
	PublicInputs  [][]byte     `json:"publicInputs"`
	VerifyingKey  []byte       `json:"verifyingKey"`

	// Commitments
	InputCommitment  [32]byte  `json:"inputCommitment"`
	OutputCommitment [32]byte  `json:"outputCommitment"`
	ProgramHash      [32]byte  `json:"programHash"`
}

// Verify verifies the ZK proof
func (p *ComputeProof) Verify() error {
	// Basic validation - full ZK verification is performed by ZKVM
	if len(p.Proof) == 0 {
		return ErrAttestationFailed
	}
	return nil
}

// CommitteeCert represents threshold endorsement from a compute committee
type CommitteeCert struct {
	CommitteeID    ids.ID       `json:"committeeId"`
	Threshold      int          `json:"threshold"`
	TotalMembers   int          `json:"totalMembers"`

	// Endorsements from committee members
	Endorsements   []*Endorsement `json:"endorsements"`

	// Aggregate signature (if using BLS)
	AggregateSignature []byte   `json:"aggregateSignature,omitempty"`

	// What is being certified
	RequestID      ids.ID       `json:"requestId"`
	OutputCommitment [32]byte   `json:"outputCommitment"`
	Timestamp      time.Time    `json:"timestamp"`
}

// Endorsement is a single committee member's endorsement
type Endorsement struct {
	MemberID       []byte       `json:"memberId"`
	MemberIndex    int          `json:"memberIndex"`
	Signature      []byte       `json:"signature"`
	TEEAttestation *TEEAttestation `json:"teeAttestation,omitempty"`
}

// signingDigest is the canonical, domain-separated message that every
// committee member signs when endorsing this certificate. It binds the
// committee, the request, the certified output commitment and the
// timestamp so that an endorsement for one (request, output) pair can
// never be replayed for another.
func (c *CommitteeCert) signingDigest() [32]byte {
	h := sha256.New()
	h.Write([]byte("lux/chainadapter/committee-cert/v1"))
	h.Write(c.CommitteeID[:])
	h.Write(c.RequestID[:])
	h.Write(c.OutputCommitment[:])
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(c.Timestamp.UTC().UnixNano()))
	h.Write(ts[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Verify checks that the certificate carries at least Threshold valid,
// distinct endorsement signatures from members of the supplied committee.
//
// Each endorsement must:
//   - reference a member that exists in the committee roster (known signer);
//   - carry a MemberID matching the roster entry at that index;
//   - be a BLS signature over the certificate's canonical signing digest
//     that verifies under that member's registered public key;
//   - be distinct — no member may endorse twice.
//
// The certificate is rejected (fail-closed) on any nil/malformed, unknown,
// duplicate, or cryptographically invalid endorsement, on a committee/cert
// parameter mismatch, or when fewer than Threshold distinct valid
// endorsements are present. There is no count-only path: every accepted
// endorsement has had its signature verified against a registered key.
func (c *CommitteeCert) Verify(committee *ComputeCommittee) error {
	if committee == nil {
		return ErrCommitteeCertInvalid
	}
	// Threshold and committee identity must agree, and the roster must be
	// internally consistent (one public key per member).
	if c.Threshold <= 0 || c.Threshold != committee.Threshold {
		return ErrCommitteeCertInvalid
	}
	if c.CommitteeID != committee.ID {
		return ErrCommitteeCertInvalid
	}
	if len(committee.PublicKeys) != len(committee.Members) || len(committee.PublicKeys) == 0 {
		return ErrCommitteeCertInvalid
	}
	if len(c.Endorsements) < c.Threshold {
		return ErrCommitteeCertInvalid
	}

	msg := c.signingDigest()
	seen := make(map[int]struct{}, len(c.Endorsements))

	for _, e := range c.Endorsements {
		if e == nil {
			return ErrCommitteeCertInvalid
		}
		idx := e.MemberIndex
		if idx < 0 || idx >= len(committee.PublicKeys) {
			return ErrCommitteeCertInvalid // unknown signer
		}
		if _, dup := seen[idx]; dup {
			return ErrCommitteeCertInvalid // duplicate signer
		}
		if !bytes.Equal(e.MemberID, committee.Members[idx]) {
			return ErrCommitteeCertInvalid // identity does not match roster index
		}
		pk, err := bls.PublicKeyFromCompressedBytes(committee.PublicKeys[idx])
		if err != nil {
			return ErrCommitteeCertInvalid
		}
		sig, err := bls.SignatureFromBytes(e.Signature)
		if err != nil {
			return ErrCommitteeCertInvalid
		}
		if !bls.Verify(pk, sig, msg[:]) {
			return ErrCommitteeCertInvalid // forged or invalid signature
		}
		seen[idx] = struct{}{}
	}

	if len(seen) < c.Threshold {
		return ErrCommitteeCertInvalid
	}
	return nil
}

// ConfidentialComputeEngine handles confidential computation
type ConfidentialComputeEngine struct {
	mu sync.RWMutex

	// TEE configuration
	teeType       TEEType
	teeAvailable  bool

	// Active compute sessions
	sessions map[ids.ID]*ComputeSession

	// Result cache
	results       map[ids.ID]*ComputeResult

	// Registered committee rosters, keyed by committee ID. A committee
	// certificate can only be verified against a roster registered here;
	// an unknown committee fails closed.
	committees map[ids.ID]*ComputeCommittee
}

// ComputeSession tracks an active computation
type ComputeSession struct {
	Request       *ComputeRequest
	Status        ComputeStatus
	StartedAt     time.Time
	Endorsements  []*Endorsement
}

// ComputeCommittee is a group that collectively attests to computation
type ComputeCommittee struct {
	ID            ids.ID
	Members       [][]byte
	Threshold     int
	PublicKeys    [][]byte
}

// NewConfidentialComputeEngine creates a new compute engine
func NewConfidentialComputeEngine(teeType TEEType) *ConfidentialComputeEngine {
	return &ConfidentialComputeEngine{
		teeType:      teeType,
		teeAvailable: true, // Check actual TEE availability
		sessions:     make(map[ids.ID]*ComputeSession),
		results:      make(map[ids.ID]*ComputeResult),
		committees:   make(map[ids.ID]*ComputeCommittee),
	}
}

// RegisterCommittee registers a committee roster so that certificates the
// committee produces can be verified against its members' public keys.
// PublicKeys and Members must be parallel arrays (one compressed BLS public
// key per member).
func (e *ConfidentialComputeEngine) RegisterCommittee(committee *ComputeCommittee) error {
	if committee == nil || len(committee.PublicKeys) != len(committee.Members) || len(committee.PublicKeys) == 0 {
		return ErrCommitteeCertInvalid
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.committees[committee.ID] = committee
	return nil
}

// SubmitRequest submits a compute request
func (e *ConfidentialComputeEngine) SubmitRequest(ctx context.Context, req *ComputeRequest) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if req.RequireTEE && !e.teeAvailable {
		return ErrTEENotAvailable
	}

	session := &ComputeSession{
		Request:   req,
		Status:    ComputeStatusPending,
		StartedAt: time.Now(),
	}

	e.sessions[req.ID] = session
	return nil
}

// Execute executes a computation inside TEE
func (e *ConfidentialComputeEngine) Execute(ctx context.Context, reqID ids.ID) (*ComputeResult, error) {
	e.mu.Lock()
	session, exists := e.sessions[reqID]
	if !exists {
		e.mu.Unlock()
		return nil, errors.New("session not found")
	}
	session.Status = ComputeStatusRunning
	e.mu.Unlock()

	req := session.Request

	// Execute computation based on mode
	var result *ComputeResult
	var err error

	switch req.ComputeMode {
	case ComputeModeTEE:
		result, err = e.executeTEE(ctx, req)
	case ComputeModeFHE:
		result, err = e.executeFHE(ctx, req)
	case ComputeModeZK:
		result, err = e.executeZK(ctx, req)
	case ComputeModeHybrid:
		result, err = e.executeHybrid(ctx, req)
	default:
		err = errors.New("unsupported compute mode")
	}

	e.mu.Lock()
	if err != nil {
		session.Status = ComputeStatusFailed
	} else {
		session.Status = ComputeStatusCompleted
		e.results[reqID] = result
	}
	e.mu.Unlock()

	return result, err
}

// executeTEE executes computation inside a TEE
func (e *ConfidentialComputeEngine) executeTEE(ctx context.Context, req *ComputeRequest) (*ComputeResult, error) {
	// In production:
	// 1. Load encrypted inputs into TEE
	// 2. Decrypt inside TEE using sealed keys
	// 3. Execute program
	// 4. Encrypt outputs
	// 5. Generate attestation quote

	// Simulate TEE execution
	outputCommitment := sha256.Sum256(req.InputCommitment[:])

	attestation := &TEEAttestation{
		TEEType:          e.teeType,
		Quote:            make([]byte, 256), // Simulated quote
		QuoteVersion:     3,
		InputCommitment:  req.InputCommitment,
		OutputCommitment: outputCommitment,
		ProgramHash:      req.ProgramHash,
		Timestamp:        time.Now(),
		Expiry:           time.Now().Add(24 * time.Hour),
	}
	attestation.ReportData = computeReportData(attestation.InputCommitment, attestation.OutputCommitment, attestation.ProgramHash)

	return &ComputeResult{
		RequestID:        req.ID,
		Status:           ComputeStatusCompleted,
		OutputData:       []byte{}, // Encrypted output
		OutputCommitment: outputCommitment,
		Attestation:      attestation,
		ComputedAt:       time.Now(),
	}, nil
}

// executeFHE executes FHE computation
func (e *ConfidentialComputeEngine) executeFHE(ctx context.Context, req *ComputeRequest) (*ComputeResult, error) {
	// In production:
	// 1. Perform homomorphic operations on encrypted data
	// 2. Return encrypted result (no decryption needed)

	outputCommitment := sha256.Sum256(req.InputCommitment[:])

	return &ComputeResult{
		RequestID:        req.ID,
		Status:           ComputeStatusCompleted,
		OutputData:       []byte{}, // FHE encrypted output
		OutputCommitment: outputCommitment,
		ComputedAt:       time.Now(),
	}, nil
}

// executeZK executes computation with ZK proof
func (e *ConfidentialComputeEngine) executeZK(ctx context.Context, req *ComputeRequest) (*ComputeResult, error) {
	// In production:
	// 1. Execute computation
	// 2. Generate ZK proof of correct execution

	outputCommitment := sha256.Sum256(req.InputCommitment[:])

	proof := &ComputeProof{
		ProofSystem:      "plonk",
		Proof:            make([]byte, 128), // Simulated proof
		InputCommitment:  req.InputCommitment,
		OutputCommitment: outputCommitment,
		ProgramHash:      req.ProgramHash,
	}

	return &ComputeResult{
		RequestID:        req.ID,
		Status:           ComputeStatusCompleted,
		OutputData:       []byte{},
		OutputCommitment: outputCommitment,
		Proof:            proof,
		ComputedAt:       time.Now(),
	}, nil
}

// executeHybrid executes with hybrid TEE+ZK or TEE+FHE
func (e *ConfidentialComputeEngine) executeHybrid(ctx context.Context, req *ComputeRequest) (*ComputeResult, error) {
	// Execute in TEE first
	result, err := e.executeTEE(ctx, req)
	if err != nil {
		return nil, err
	}

	// Optionally add ZK proof
	if req.RequireProof {
		proof := &ComputeProof{
			ProofSystem:      "plonk",
			Proof:            make([]byte, 128),
			InputCommitment:  req.InputCommitment,
			OutputCommitment: result.OutputCommitment,
			ProgramHash:      req.ProgramHash,
		}
		result.Proof = proof
	}

	return result, nil
}

// GetResult retrieves a compute result
func (e *ConfidentialComputeEngine) GetResult(reqID ids.ID) (*ComputeResult, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result, exists := e.results[reqID]
	return result, exists
}

// VerifyResult verifies a compute result
func (e *ConfidentialComputeEngine) VerifyResult(result *ComputeResult) error {
	// Verify attestation if present
	if result.Attestation != nil {
		if err := result.Attestation.Verify(); err != nil {
			return err
		}
	}

	// Verify proof if present
	if result.Proof != nil {
		if err := result.Proof.Verify(); err != nil {
			return err
		}
	}

	// Verify committee cert if present. The roster must have been
	// registered; an unknown committee fails closed.
	if result.CommitteeCert != nil {
		e.mu.RLock()
		committee := e.committees[result.CommitteeCert.CommitteeID]
		e.mu.RUnlock()
		if committee == nil {
			return ErrCommitteeCertInvalid
		}
		if err := result.CommitteeCert.Verify(committee); err != nil {
			return err
		}
	}

	return nil
}

// ComputeAnchor anchors compute result to chain
type ComputeAnchor struct {
	ResultID         ids.ID    `json:"resultId"`
	RequestID        ids.ID    `json:"requestId"`
	AppChainID       ids.ID    `json:"appChainId"`

	// Commitments
	InputCommitment  [32]byte  `json:"inputCommitment"`
	OutputCommitment [32]byte  `json:"outputCommitment"`
	ProgramHash      [32]byte  `json:"programHash"`

	// Attestation summary
	TEEType          TEEType   `json:"teeType,omitempty"`
	HasTEEAttest     bool      `json:"hasTeeAttest"`
	HasZKProof       bool      `json:"hasZkProof"`
	HasCommitteeCert bool      `json:"hasCommitteeCert"`

	// DA reference for full attestation
	AttestationDAPointer *DAPointer `json:"attestationDaPointer"`

	// Block anchoring
	BlockHeight      uint64    `json:"blockHeight"`
	BlockHash        [32]byte  `json:"blockHash"`
	TxIndex          uint32    `json:"txIndex"`
	Timestamp        time.Time `json:"timestamp"`
}

// Hash returns the hash of the compute anchor
func (a *ComputeAnchor) Hash() [32]byte {
	h := sha256.New()
	h.Write(a.ResultID[:])
	h.Write(a.RequestID[:])
	h.Write(a.AppChainID[:])
	h.Write(a.InputCommitment[:])
	h.Write(a.OutputCommitment[:])
	h.Write(a.ProgramHash[:])
	binary.Write(h, binary.BigEndian, a.BlockHeight)
	h.Write(a.BlockHash[:])

	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}
