// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fhe

import (
	"testing"
	"time"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/stretchr/testify/require"
)

func newTestRegistry(t *testing.T) *Registry {
	db := memdb.New()
	registry, err := NewRegistry(db)
	require.NoError(t, err)
	return registry
}

func TestRegistryInit(t *testing.T) {
	registry := newTestRegistry(t)
	require.NotNil(t, registry)
	require.Equal(t, uint64(0), registry.GetCurrentEpoch())
}

func TestRegistryCiphertextMeta(t *testing.T) {
	registry := newTestRegistry(t)

	meta := &CiphertextMeta{
		Handle:  [32]byte{1, 2, 3, 4},
		Owner:   [20]byte{0xaa, 0xbb},
		Type:    1,
		Level:   14,
		Size:    1024,
		ChainID: ids.GenerateTestID(),
	}

	// Register ciphertext
	err := registry.RegisterCiphertext(meta)
	require.NoError(t, err)

	// Retrieve ciphertext
	retrieved, err := registry.GetCiphertextMeta(meta.Handle)
	require.NoError(t, err)
	require.Equal(t, meta.Handle, retrieved.Handle)
	require.Equal(t, meta.Owner, retrieved.Owner)
	require.Equal(t, meta.Type, retrieved.Type)
	require.Equal(t, meta.Level, retrieved.Level)
	require.Equal(t, meta.Size, retrieved.Size)

	// Test not found
	_, err = registry.GetCiphertextMeta([32]byte{0xff})
	require.ErrorIs(t, err, ErrCiphertextNotFound)

	// Delete ciphertext
	err = registry.DeleteCiphertext(meta.Handle)
	require.NoError(t, err)

	_, err = registry.GetCiphertextMeta(meta.Handle)
	require.ErrorIs(t, err, ErrCiphertextNotFound)
}

func TestRegistryDecryptRequest(t *testing.T) {
	registry := newTestRegistry(t)

	req := &DecryptRequest{
		RequestID:        [32]byte{1, 2, 3, 4, 5, 6, 7, 8},
		CiphertextHandle: [32]byte{0xaa, 0xbb},
		Requester:        [20]byte{0x11, 0x22},
		Callback:         [20]byte{0x33, 0x44},
		CallbackSelector: [4]byte{0xa, 0xb, 0xc, 0xd},
		SourceChain:      ids.GenerateTestID(),
		Nonce:            1,
		Expiry:           time.Now().Add(time.Hour).Unix(),
	}

	// Create request
	err := registry.CreateDecryptRequest(req)
	require.NoError(t, err)

	// Retrieve request
	retrieved, err := registry.GetDecryptRequest(req.RequestID)
	require.NoError(t, err)
	require.Equal(t, req.RequestID, retrieved.RequestID)
	require.Equal(t, req.CiphertextHandle, retrieved.CiphertextHandle)
	require.Equal(t, RequestPending, retrieved.Status)

	// Update status
	resultHandle := [32]byte{0xde, 0xad, 0xbe, 0xef}
	err = registry.UpdateDecryptRequest(req.RequestID, RequestCompleted, resultHandle, "")
	require.NoError(t, err)

	retrieved, err = registry.GetDecryptRequest(req.RequestID)
	require.NoError(t, err)
	require.Equal(t, RequestCompleted, retrieved.Status)
	require.Equal(t, resultHandle, retrieved.ResultHandle)

	// Test not found
	_, err = registry.GetDecryptRequest([32]byte{0xff})
	require.ErrorIs(t, err, ErrRequestNotFound)
}

func TestRegistryPermit(t *testing.T) {
	registry := newTestRegistry(t)

	permitID := [32]byte{0x11, 0x22, 0x33}
	handle := [32]byte{0x44, 0x55}
	grantee := [20]byte{0xcc, 0xdd}
	grantor := [20]byte{0xaa, 0xbb}

	// First register the ciphertext (required for ownership verification)
	ctMeta := &CiphertextMeta{
		Handle:  handle,
		Owner:   grantor, // Owner must match permit grantor
		Type:    1,
		Level:   0,
		Size:    1024,
		ChainID: ids.GenerateTestID(),
	}
	err := registry.RegisterCiphertext(ctMeta)
	require.NoError(t, err)

	permit := &Permit{
		PermitID:   permitID,
		Handle:     handle,
		Grantee:    grantee,
		Grantor:    grantor,
		Operations: PermitOpDecrypt | PermitOpReencrypt,
		Expiry:     time.Now().Add(time.Hour).Unix(),
		ChainID:    ids.GenerateTestID(),
	}

	// Create permit
	err = registry.CreatePermit(permit)
	require.NoError(t, err)

	// Get permit
	retrieved, err := registry.GetPermit(permit.PermitID)
	require.NoError(t, err)
	require.Equal(t, permit.PermitID, retrieved.PermitID)
	require.Equal(t, permit.Handle, retrieved.Handle)
	require.Equal(t, permit.Grantee, retrieved.Grantee)

	// Verify permit - valid operation
	err = registry.VerifyPermit(permitID, handle, grantee, PermitOpDecrypt)
	require.NoError(t, err)

	// Verify permit - wrong handle
	err = registry.VerifyPermit(permitID, [32]byte{0xff}, grantee, PermitOpDecrypt)
	require.Error(t, err)

	// Verify permit - wrong grantee
	err = registry.VerifyPermit(permitID, handle, [20]byte{0xff}, PermitOpDecrypt)
	require.Error(t, err)

	// Verify permit - disallowed operation
	err = registry.VerifyPermit(permitID, handle, grantee, PermitOpTransfer)
	require.Error(t, err)
}

func TestRegistryEpoch(t *testing.T) {
	registry := newTestRegistry(t)

	info := &EpochInfo{
		Epoch:     1,
		StartTime: time.Now().Unix(),
		Threshold: 67,
		PublicKey: []byte{0x04, 0xaa, 0xbb, 0xcc},
		Status:    EpochActive,
	}

	// Set epoch
	err := registry.SetEpoch(1, info)
	require.NoError(t, err)

	// Get epoch
	retrieved, err := registry.GetEpoch(1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), retrieved.Epoch)
	require.Equal(t, 67, retrieved.Threshold)

	// Current epoch should be updated
	require.Equal(t, uint64(1), registry.GetCurrentEpoch())

	// Set higher epoch
	info2 := &EpochInfo{Epoch: 2, Threshold: 67, Status: EpochActive}
	err = registry.SetEpoch(2, info2)
	require.NoError(t, err)
	require.Equal(t, uint64(2), registry.GetCurrentEpoch())
}

// TestRegistryCommittee is covered by TestRegistryCommitteeFromEpoch
// since committee is embedded in EpochInfo

func TestRequestStatusString(t *testing.T) {
	require.Equal(t, "pending", RequestPending.String())
	require.Equal(t, "processing", RequestProcessing.String())
	require.Equal(t, "completed", RequestCompleted.String())
	require.Equal(t, "failed", RequestFailed.String())
	require.Equal(t, "expired", RequestExpired.String())
	require.Equal(t, "unknown", RequestStatus(99).String())
}

func TestRegistrySession(t *testing.T) {
	registry := newTestRegistry(t)

	session := &SessionState{
		SessionID:        "test-session-1",
		CiphertextHandle: [32]byte{0x11, 0x22},
		Threshold:        3,
		Participants:     []ids.NodeID{ids.GenerateTestNodeID(), ids.GenerateTestNodeID()},
		SharesReceived:   0,
		Status:           SessionActive,
	}

	// Save session
	err := registry.SaveSession(session)
	require.NoError(t, err)
	require.NotZero(t, session.CreatedAt)

	// Retrieve session
	retrieved, err := registry.GetSession("test-session-1")
	require.NoError(t, err)
	require.Equal(t, session.SessionID, retrieved.SessionID)
	require.Equal(t, session.Threshold, retrieved.Threshold)
	require.Equal(t, SessionActive, retrieved.Status)

	// Update session
	session.SharesReceived = 2
	session.Status = SessionCompleted
	err = registry.SaveSession(session)
	require.NoError(t, err)

	retrieved, err = registry.GetSession("test-session-1")
	require.NoError(t, err)
	require.Equal(t, 2, retrieved.SharesReceived)
	require.Equal(t, SessionCompleted, retrieved.Status)

	// Delete session
	err = registry.DeleteSession("test-session-1")
	require.NoError(t, err)

	// Should be gone
	_, err = registry.GetSession("test-session-1")
	require.ErrorIs(t, err, ErrSessionNotFound)
}

func TestRegistryRevokePermit(t *testing.T) {
	registry := newTestRegistry(t)

	permitID := [32]byte{0x11, 0x22, 0x33}
	permit := &Permit{
		PermitID:   permitID,
		Handle:     [32]byte{0x44, 0x55},
		Grantee:    [20]byte{0xcc, 0xdd},
		Grantor:    [20]byte{0xaa, 0xbb},
		Operations: PermitOpDecrypt,
		Expiry:     time.Now().Add(time.Hour).Unix(),
		ChainID:    ids.GenerateTestID(),
	}

	// Create permit
	err := registry.CreatePermit(permit)
	require.NoError(t, err)

	// Verify it exists
	_, err = registry.GetPermit(permitID)
	require.NoError(t, err)

	// Revoke permit
	err = registry.RevokePermit(permitID)
	require.NoError(t, err)

	// Should be gone
	_, err = registry.GetPermit(permitID)
	require.ErrorIs(t, err, ErrPermitNotFound)
}

func TestRegistryPermitExpired(t *testing.T) {
	registry := newTestRegistry(t)

	// First register a ciphertext
	ctMeta := &CiphertextMeta{
		Handle: [32]byte{0x44, 0x55},
		Owner:  [20]byte{0xaa, 0xbb},
		Type:   1,
	}
	require.NoError(t, registry.RegisterCiphertext(ctMeta))

	permitID := [32]byte{0x11, 0x22, 0x33}
	permit := &Permit{
		PermitID:   permitID,
		Handle:     [32]byte{0x44, 0x55},
		Grantee:    [20]byte{0xcc, 0xdd},
		Grantor:    [20]byte{0xaa, 0xbb},
		Operations: PermitOpDecrypt,
		Expiry:     time.Now().Add(-time.Hour).Unix(), // Already expired
		ChainID:    ids.GenerateTestID(),
	}

	require.NoError(t, registry.CreatePermit(permit))

	// Verify should fail due to expiry
	err := registry.VerifyPermit(permitID, permit.Handle, permit.Grantee, PermitOpDecrypt)
	require.ErrorIs(t, err, ErrPermitExpired)
}

func TestRegistryVerifyPermitGrantorNotOwner(t *testing.T) {
	registry := newTestRegistry(t)

	// Register ciphertext with owner
	owner := [20]byte{0xaa, 0xbb}
	ctMeta := &CiphertextMeta{
		Handle: [32]byte{0x44, 0x55},
		Owner:  owner,
		Type:   1,
	}
	require.NoError(t, registry.RegisterCiphertext(ctMeta))

	// Create permit with different grantor (not the owner)
	permitID := [32]byte{0x11, 0x22, 0x33}
	permit := &Permit{
		PermitID:   permitID,
		Handle:     [32]byte{0x44, 0x55},
		Grantee:    [20]byte{0xcc, 0xdd},
		Grantor:    [20]byte{0xff, 0xff}, // Different from owner
		Operations: PermitOpDecrypt,
		Expiry:     time.Now().Add(time.Hour).Unix(),
	}
	require.NoError(t, registry.CreatePermit(permit))

	// Verify should fail - grantor doesn't own the ciphertext
	err := registry.VerifyPermit(permitID, permit.Handle, permit.Grantee, PermitOpDecrypt)
	require.ErrorIs(t, err, ErrPermitInvalid)
}

func TestRegistryClose(t *testing.T) {
	registry := newTestRegistry(t)
	err := registry.Close()
	require.NoError(t, err)
}

func TestRegistryDecryptRequestUpdateNotFound(t *testing.T) {
	registry := newTestRegistry(t)

	// Try to update non-existent request
	err := registry.UpdateDecryptRequest([32]byte{0xff}, RequestCompleted, [32]byte{}, "")
	require.ErrorIs(t, err, ErrRequestNotFound)
}

func TestRegistryAddCommitteeMemberUpdate(t *testing.T) {
	registry := newTestRegistry(t)

	// Set initial epoch with a committee member
	nodeID := ids.GenerateTestNodeID()
	member := &CommitteeMember{
		NodeID:    nodeID,
		PublicKey: []byte("pk1"),
		Weight:    100,
		Index:     0,
	}
	require.NoError(t, registry.AddCommitteeMember(member))

	// Update same member with new weight
	member.Weight = 200
	require.NoError(t, registry.AddCommitteeMember(member))

	// Verify weight was updated
	members, err := registry.GetCommittee()
	require.NoError(t, err)
	require.Equal(t, 1, len(members))
	require.Equal(t, uint64(200), members[0].Weight)
}
