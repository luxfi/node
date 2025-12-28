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

	permit := &Permit{
		PermitID:   permitID,
		Handle:     handle,
		Grantee:    grantee,
		Grantor:    [20]byte{0xaa, 0xbb},
		Operations: PermitOpDecrypt | PermitOpReencrypt,
		Expiry:     time.Now().Add(time.Hour).Unix(),
		ChainID:    ids.GenerateTestID(),
	}

	// Create permit
	err := registry.CreatePermit(permit)
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
