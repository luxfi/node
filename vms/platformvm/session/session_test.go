// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package session

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
)

func TestComputeSessionID(t *testing.T) {
	serviceID := ids.GenerateTestID()
	epoch := uint64(100)
	txID := ids.GenerateTestID()

	sessionID1 := ComputeSessionID(serviceID, epoch, txID)
	sessionID2 := ComputeSessionID(serviceID, epoch, txID)

	// Same inputs should produce same session ID
	require.Equal(t, sessionID1, sessionID2)

	// Different inputs should produce different session IDs
	sessionID3 := ComputeSessionID(serviceID, epoch+1, txID)
	require.NotEqual(t, sessionID1, sessionID3)
}

func TestComputeOracleRequestID(t *testing.T) {
	serviceID := ids.GenerateTestID()
	var sessionID [32]byte
	tempID := ids.GenerateTestID()
	copy(sessionID[:], tempID[:])
	step := uint32(0)
	retry := uint32(0)
	txID := ids.GenerateTestID()

	requestID1 := ComputeOracleRequestID(serviceID, sessionID, step, retry, txID)
	requestID2 := ComputeOracleRequestID(serviceID, sessionID, step, retry, txID)

	// Same inputs should produce same request ID (deterministic)
	require.Equal(t, requestID1, requestID2)

	// Different step should produce different request ID
	requestID3 := ComputeOracleRequestID(serviceID, sessionID, step+1, retry, txID)
	require.NotEqual(t, requestID1, requestID3)

	// Different retry should produce different request ID
	requestID4 := ComputeOracleRequestID(serviceID, sessionID, step, retry+1, txID)
	require.NotEqual(t, requestID1, requestID4)
}

func TestSessionManager_CreateSession(t *testing.T) {
	manager := NewManager()

	serviceID := ids.GenerateTestID()
	epoch := uint64(100)
	txID := ids.GenerateTestID()
	committee := []ids.NodeID{
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
	}

	session, err := manager.CreateSession(serviceID, epoch, txID, committee)
	require.NoError(t, err)
	require.NotNil(t, session)
	require.Equal(t, StatePending, session.State)
	require.Equal(t, serviceID, session.ServiceID)
	require.Equal(t, epoch, session.Epoch)
	require.Equal(t, 3, len(session.Committee))

	// Duplicate session should fail
	_, err = manager.CreateSession(serviceID, epoch, txID, committee)
	require.Error(t, err)
}

func TestSessionManager_SessionLifecycle(t *testing.T) {
	manager := NewManager()

	// Create session
	serviceID := ids.GenerateTestID()
	epoch := uint64(100)
	txID := ids.GenerateTestID()
	committee := []ids.NodeID{ids.GenerateTestNodeID()}

	session, err := manager.CreateSession(serviceID, epoch, txID, committee)
	require.NoError(t, err)
	require.Equal(t, StatePending, session.State)

	// Start session
	err = manager.StartSession(session.SessionID)
	require.NoError(t, err)

	// Retrieve session
	retrieved, err := manager.GetSession(session.SessionID)
	require.NoError(t, err)
	require.Equal(t, StateRunning, retrieved.State)

	// Create oracle request (external write)
	stepTxID := ids.GenerateTestID()
	var inputHash [32]byte
	inputHashID := ids.GenerateTestID()
	copy(inputHash[:], inputHashID[:])

	requestID, err := manager.CreateOracleRequest(session.SessionID, StepKindWriteExternal, stepTxID, inputHash)
	require.NoError(t, err)
	require.NotEqual(t, [32]byte{}, requestID)

	// Session should now be waiting for I/O
	retrieved, err = manager.GetSession(session.SessionID)
	require.NoError(t, err)
	require.Equal(t, StateWaitingIO, retrieved.State)
	require.Equal(t, 1, len(retrieved.Steps))
	require.Equal(t, StepKindWriteExternal, retrieved.Steps[0].Kind)

	// Complete the step with attestation
	var oracleCommitRoot, attestationID, outputHash [32]byte
	oracleCommitRootID := ids.GenerateTestID()
	attestationIDGen := ids.GenerateTestID()
	outputHashID := ids.GenerateTestID()
	copy(oracleCommitRoot[:], oracleCommitRootID[:])
	copy(attestationID[:], attestationIDGen[:])
	copy(outputHash[:], outputHashID[:])

	err = manager.CompleteStep(session.SessionID, 0, oracleCommitRoot, attestationID, outputHash)
	require.NoError(t, err)

	// Session should be running again
	retrieved, err = manager.GetSession(session.SessionID)
	require.NoError(t, err)
	require.Equal(t, StateRunning, retrieved.State)
	require.Equal(t, StepStateCompleted, retrieved.Steps[0].State)

	// Finalize session
	var finalOutput, oracleRoot, receiptsRoot [32]byte
	finalOutputID := ids.GenerateTestID()
	oracleRootID := ids.GenerateTestID()
	receiptsRootID := ids.GenerateTestID()
	copy(finalOutput[:], finalOutputID[:])
	copy(oracleRoot[:], oracleRootID[:])
	copy(receiptsRoot[:], receiptsRootID[:])

	err = manager.FinalizeSession(session.SessionID, finalOutput, oracleRoot, receiptsRoot, attestationID)
	require.NoError(t, err)

	// Verify finalized state
	retrieved, err = manager.GetSession(session.SessionID)
	require.NoError(t, err)
	require.Equal(t, StateFinalized, retrieved.State)
	require.Equal(t, finalOutput, retrieved.OutputHash)
	require.Equal(t, oracleRoot, retrieved.OracleRoot)
	require.Equal(t, receiptsRoot, retrieved.ReceiptsRoot)
}

func TestSessionManager_OracleRequestTracking(t *testing.T) {
	manager := NewManager()

	serviceID := ids.GenerateTestID()
	epoch := uint64(100)
	txID := ids.GenerateTestID()
	committee := []ids.NodeID{ids.GenerateTestNodeID()}

	session, err := manager.CreateSession(serviceID, epoch, txID, committee)
	require.NoError(t, err)

	err = manager.StartSession(session.SessionID)
	require.NoError(t, err)

	// Create multiple oracle requests
	var inputHash [32]byte
	_, err = manager.CreateOracleRequest(session.SessionID, StepKindWriteExternal, ids.GenerateTestID(), inputHash)
	require.NoError(t, err)

	// Get pending requests
	pending := manager.GetPendingRequestsForSession(session.SessionID)
	require.Equal(t, 1, len(pending))
	require.Equal(t, StepKindWriteExternal, pending[0].Kind)
}

func TestValidateAttestationForStep(t *testing.T) {
	step := &Step{
		StepIndex: 0,
		Kind:      StepKindWriteExternal,
		RequestID: [32]byte{1, 2, 3},
	}

	// Valid attestation
	err := ValidateAttestationForStep(
		step,
		"oracle/write",
		step.RequestID,
		[32]byte{},
	)
	require.NoError(t, err)

	// Wrong domain
	err = ValidateAttestationForStep(
		step,
		"oracle/read",
		step.RequestID,
		[32]byte{},
	)
	require.Error(t, err)

	// Wrong subject ID
	err = ValidateAttestationForStep(
		step,
		"oracle/write",
		[32]byte{9, 9, 9},
		[32]byte{},
	)
	require.Error(t, err)
}

func TestSessionManager_FailSession(t *testing.T) {
	manager := NewManager()

	serviceID := ids.GenerateTestID()
	epoch := uint64(100)
	txID := ids.GenerateTestID()
	committee := []ids.NodeID{ids.GenerateTestNodeID()}

	session, err := manager.CreateSession(serviceID, epoch, txID, committee)
	require.NoError(t, err)

	err = manager.StartSession(session.SessionID)
	require.NoError(t, err)

	// Create a pending request
	var inputHash [32]byte
	_, err = manager.CreateOracleRequest(session.SessionID, StepKindWriteExternal, ids.GenerateTestID(), inputHash)
	require.NoError(t, err)

	// Fail the session
	err = manager.FailSession(session.SessionID, "test failure")
	require.NoError(t, err)

	// Verify failed state
	retrieved, err := manager.GetSession(session.SessionID)
	require.NoError(t, err)
	require.Equal(t, StateFailed, retrieved.State)
	require.Equal(t, "test failure", retrieved.Error)

	// Pending requests should be cleaned up
	pending := manager.GetPendingRequestsForSession(session.SessionID)
	require.Equal(t, 0, len(pending))
}
