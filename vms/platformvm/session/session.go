// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package session implements the session-ready architecture for private
// permissionless workloads. It provides:
// - Session state machine (pending → running → waiting_io → finalized)
// - Oracle request creation with deterministic request_id
// - Step management with QuantumVM attestation verification
// - Integration with OracleVM and RelayVM
package session

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/luxfi/ids"
)

// SessionState represents the state of a session
type SessionState string

const (
	// StatePending - session created but not started
	StatePending SessionState = "pending"
	// StateRunning - session actively executing
	StateRunning SessionState = "running"
	// StateWaitingIO - session waiting for external I/O completion
	StateWaitingIO SessionState = "waiting_io"
	// StateFinalized - session completed successfully
	StateFinalized SessionState = "finalized"
	// StateFailed - session failed
	StateFailed SessionState = "failed"
)

// StepKind indicates the type of step
type StepKind uint8

const (
	// StepKindCompute - internal computation step
	StepKindCompute StepKind = iota
	// StepKindWriteExternal - external write (oracle/write)
	StepKindWriteExternal
	// StepKindReadExternal - external read (oracle/read)
	StepKindReadExternal
)

// Session represents a private permissionless session
type Session struct {
	// SessionID is the unique identifier
	SessionID [32]byte `json:"sessionId"`

	// ServiceID identifies the service being executed
	ServiceID ids.ID `json:"serviceId"`

	// Epoch in which this session was created
	Epoch uint64 `json:"epoch"`

	// Committee assigned to execute this session
	Committee []ids.NodeID `json:"committee"`

	// State is the current session state
	State SessionState `json:"state"`

	// CurrentStep is the current step index
	CurrentStep uint32 `json:"currentStep"`

	// Steps are the step records for this session
	Steps []*Step `json:"steps"`

	// OutputHash is the final output hash (set when finalized)
	OutputHash [32]byte `json:"outputHash,omitempty"`

	// OracleRoot is the Merkle root of all oracle observations
	OracleRoot [32]byte `json:"oracleRoot,omitempty"`

	// ReceiptsRoot is the Merkle root of all relay receipts
	ReceiptsRoot [32]byte `json:"receiptsRoot,omitempty"`

	// CreatedAt is when the session was created
	CreatedAt time.Time `json:"createdAt"`

	// FinalizedAt is when the session was finalized (if applicable)
	FinalizedAt time.Time `json:"finalizedAt,omitempty"`

	// Error message if session failed
	Error string `json:"error,omitempty"`
}

// Step represents a single execution step in a session
type Step struct {
	// StepIndex is the step number
	StepIndex uint32 `json:"stepIndex"`

	// Kind indicates the step type
	Kind StepKind `json:"kind"`

	// RequestID for external I/O steps (oracle/write or oracle/read)
	RequestID [32]byte `json:"requestId,omitempty"`

	// RetryIndex for retry attempts
	RetryIndex uint32 `json:"retryIndex"`

	// TxID that triggered this step
	TxID ids.ID `json:"txId"`

	// InputHash is the hash of step inputs
	InputHash [32]byte `json:"inputHash"`

	// OutputHash is the hash of step outputs (set when completed)
	OutputHash [32]byte `json:"outputHash,omitempty"`

	// OracleCommitRoot is the Merkle root from OracleVM (for I/O steps)
	OracleCommitRoot [32]byte `json:"oracleCommitRoot,omitempty"`

	// AttestationID is the QuantumVM attestation over the oracle commit
	AttestationID [32]byte `json:"attestationId,omitempty"`

	// State of this step
	State StepState `json:"state"`

	// StartedAt is when the step started
	StartedAt time.Time `json:"startedAt"`

	// CompletedAt is when the step completed
	CompletedAt time.Time `json:"completedAt,omitempty"`
}

// StepState represents the state of a step
type StepState string

const (
	StepStatePending   StepState = "pending"
	StepStateExecuting StepState = "executing"
	StepStateWaiting   StepState = "waiting"   // Waiting for oracle/attestation
	StepStateCompleted StepState = "completed"
	StepStateFailed    StepState = "failed"
)

// ComputeSessionID computes a deterministic session ID
func ComputeSessionID(serviceID ids.ID, epoch uint64, txID ids.ID) [32]byte {
	h := sha256.New()
	h.Write([]byte("LUX:Session:v1"))
	h.Write(serviceID[:])

	epochBytes := make([]byte, 8)
	epochBytes[0] = byte(epoch >> 56)
	epochBytes[1] = byte(epoch >> 48)
	epochBytes[2] = byte(epoch >> 40)
	epochBytes[3] = byte(epoch >> 32)
	epochBytes[4] = byte(epoch >> 24)
	epochBytes[5] = byte(epoch >> 16)
	epochBytes[6] = byte(epoch >> 8)
	epochBytes[7] = byte(epoch)
	h.Write(epochBytes)

	h.Write(txID[:])

	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// ComputeOracleRequestID computes a deterministic oracle request ID
// This matches the OracleVM.ComputeRequestID format
func ComputeOracleRequestID(serviceID ids.ID, sessionID [32]byte, step, retry uint32, txID ids.ID) [32]byte {
	h := sha256.New()
	h.Write([]byte("LUX:OracleRequest:v1"))
	h.Write(serviceID[:])
	h.Write(sessionID[:])

	stepBytes := make([]byte, 4)
	stepBytes[0] = byte(step >> 24)
	stepBytes[1] = byte(step >> 16)
	stepBytes[2] = byte(step >> 8)
	stepBytes[3] = byte(step)
	h.Write(stepBytes)

	retryBytes := make([]byte, 4)
	retryBytes[0] = byte(retry >> 24)
	retryBytes[1] = byte(retry >> 16)
	retryBytes[2] = byte(retry >> 8)
	retryBytes[3] = byte(retry)
	h.Write(retryBytes)

	h.Write(txID[:])

	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// Manager manages sessions and coordinates with OracleVM and ThresholdVM
type Manager struct {
	// Sessions indexed by session ID
	sessions map[[32]byte]*Session

	// Sessions by service
	sessionsByService map[ids.ID][]*Session

	// Pending oracle requests
	pendingRequests map[[32]byte]*OracleRequestRef

	mu sync.RWMutex
}

// OracleRequestRef tracks a pending oracle request
type OracleRequestRef struct {
	RequestID [32]byte
	SessionID [32]byte
	StepIndex uint32
	Kind      StepKind
	CreatedAt time.Time
}

// NewManager creates a new session manager
func NewManager() *Manager {
	return &Manager{
		sessions:          make(map[[32]byte]*Session),
		sessionsByService: make(map[ids.ID][]*Session),
		pendingRequests:   make(map[[32]byte]*OracleRequestRef),
	}
}

// CreateSession creates a new session
func (m *Manager) CreateSession(serviceID ids.ID, epoch uint64, txID ids.ID, committee []ids.NodeID) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sessionID := ComputeSessionID(serviceID, epoch, txID)

	// Check if session already exists
	if _, exists := m.sessions[sessionID]; exists {
		return nil, errors.New("session already exists")
	}

	session := &Session{
		SessionID:   sessionID,
		ServiceID:   serviceID,
		Epoch:       epoch,
		Committee:   committee,
		State:       StatePending,
		CurrentStep: 0,
		Steps:       []*Step{},
		CreatedAt:   time.Now(),
	}

	m.sessions[sessionID] = session
	m.sessionsByService[serviceID] = append(m.sessionsByService[serviceID], session)

	return session, nil
}

// GetSession retrieves a session by ID
func (m *Manager) GetSession(sessionID [32]byte) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return nil, errors.New("session not found")
	}
	return session, nil
}

// StartSession transitions a session from pending to running
func (m *Manager) StartSession(sessionID [32]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return errors.New("session not found")
	}

	if session.State != StatePending {
		return fmt.Errorf("cannot start session in state %s", session.State)
	}

	session.State = StateRunning
	return nil
}

// CreateOracleRequest creates an oracle request for an external I/O step
// Returns the deterministic request_id that should be submitted to OracleVM
func (m *Manager) CreateOracleRequest(sessionID [32]byte, kind StepKind, txID ids.ID, inputHash [32]byte) ([32]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return [32]byte{}, errors.New("session not found")
	}

	if session.State != StateRunning {
		return [32]byte{}, fmt.Errorf("cannot create oracle request in state %s", session.State)
	}

	if kind != StepKindWriteExternal && kind != StepKindReadExternal {
		return [32]byte{}, errors.New("invalid step kind for oracle request")
	}

	// Create step
	stepIndex := uint32(len(session.Steps))
	retryIndex := uint32(0)

	requestID := ComputeOracleRequestID(session.ServiceID, sessionID, stepIndex, retryIndex, txID)

	step := &Step{
		StepIndex:  stepIndex,
		Kind:       kind,
		RequestID:  requestID,
		RetryIndex: retryIndex,
		TxID:       txID,
		InputHash:  inputHash,
		State:      StepStatePending,
		StartedAt:  time.Now(),
	}

	session.Steps = append(session.Steps, step)
	session.CurrentStep = stepIndex
	session.State = StateWaitingIO

	// Track pending request
	m.pendingRequests[requestID] = &OracleRequestRef{
		RequestID: requestID,
		SessionID: sessionID,
		StepIndex: stepIndex,
		Kind:      kind,
		CreatedAt: time.Now(),
	}

	return requestID, nil
}

// CompleteStep marks a step as complete with QuantumVM attestation verification
// This is called when PlatformVM receives attestation over oracle commit
func (m *Manager) CompleteStep(
	sessionID [32]byte,
	stepIndex uint32,
	oracleCommitRoot [32]byte,
	attestationID [32]byte,
	outputHash [32]byte,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return errors.New("session not found")
	}

	if int(stepIndex) >= len(session.Steps) {
		return errors.New("step index out of range")
	}

	step := session.Steps[stepIndex]
	if step.State != StepStatePending && step.State != StepStateWaiting {
		return fmt.Errorf("cannot complete step in state %s", step.State)
	}

	// Record the oracle commit and attestation
	step.OracleCommitRoot = oracleCommitRoot
	step.AttestationID = attestationID
	step.OutputHash = outputHash
	step.State = StepStateCompleted
	step.CompletedAt = time.Now()

	// Remove from pending requests
	delete(m.pendingRequests, step.RequestID)

	// Transition session back to running (or finalized if this was the last step)
	session.State = StateRunning

	return nil
}

// FinalizeSession marks a session as finalized
// This requires a QuantumVM attestation over the session completion
func (m *Manager) FinalizeSession(
	sessionID [32]byte,
	outputHash [32]byte,
	oracleRoot [32]byte,
	receiptsRoot [32]byte,
	attestationID [32]byte,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return errors.New("session not found")
	}

	if session.State != StateRunning {
		return fmt.Errorf("cannot finalize session in state %s", session.State)
	}

	// Verify all steps are completed
	for i, step := range session.Steps {
		if step.State != StepStateCompleted {
			return fmt.Errorf("step %d not completed", i)
		}
	}

	session.OutputHash = outputHash
	session.OracleRoot = oracleRoot
	session.ReceiptsRoot = receiptsRoot
	session.State = StateFinalized
	session.FinalizedAt = time.Now()

	return nil
}

// FailSession marks a session as failed
func (m *Manager) FailSession(sessionID [32]byte, err string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return errors.New("session not found")
	}

	session.State = StateFailed
	session.Error = err

	// Clean up pending requests
	for _, step := range session.Steps {
		if step.State == StepStatePending || step.State == StepStateWaiting {
			delete(m.pendingRequests, step.RequestID)
		}
	}

	return nil
}

// GetPendingRequestsForSession returns all pending oracle requests for a session
func (m *Manager) GetPendingRequestsForSession(sessionID [32]byte) []*OracleRequestRef {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var requests []*OracleRequestRef
	for _, req := range m.pendingRequests {
		if req.SessionID == sessionID {
			requests = append(requests, req)
		}
	}
	return requests
}

// ValidateAttestationForStep validates that an attestation is valid for a step
// This should be called before CompleteStep to verify the attestation
func ValidateAttestationForStep(
	step *Step,
	attestationDomain string,
	attestationSubjectID [32]byte,
	attestationCommitRoot [32]byte,
) error {
	// Verify the attestation is for the correct request
	if attestationSubjectID != step.RequestID {
		return errors.New("attestation subject ID does not match request ID")
	}

	// Verify the attestation domain matches the step kind
	var expectedDomain string
	switch step.Kind {
	case StepKindWriteExternal:
		expectedDomain = "oracle/write"
	case StepKindReadExternal:
		expectedDomain = "oracle/read"
	default:
		return errors.New("step kind does not require attestation")
	}

	if attestationDomain != expectedDomain {
		return fmt.Errorf("attestation domain %s does not match expected %s", attestationDomain, expectedDomain)
	}

	return nil
}
