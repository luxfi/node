// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fhe

import (
	"context"
	"testing"
	"time"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/stretchr/testify/require"
)

func newTestTFHEService(t *testing.T) *TFHEService {
	require := require.New(t)

	db := memdb.New()
	reg, err := NewRegistry(db)
	require.NoError(err)

	// Initialize epoch with committee
	committee := []CommitteeMember{
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk2"), Weight: 100, Index: 1},
	}
	epochInfo := &EpochInfo{
		Epoch:     1,
		StartTime: time.Now().Unix(),
		Threshold: 67,
		PublicKey: []byte("test-public-key"),
		Committee: committee,
		Status:    EpochActive,
	}
	err = reg.SetEpoch(1, epochInfo)
	require.NoError(err)

	service := &TFHEService{
		logger:   log.NewNoOpLogger(),
		registry: reg,
		chainID:  ids.GenerateTestID(),
	}

	return service
}

func TestTFHEServiceGetPublicParams(t *testing.T) {
	require := require.New(t)

	service := newTestTFHEService(t)

	args := &GetPublicParamsArgs{}
	reply := &GetPublicParamsReply{}

	err := service.GetPublicParams(context.Background(), args, reply)
	require.NoError(err)

	require.Equal(uint64(1), reply.Epoch)
	require.Equal(67, reply.Threshold)
	require.NotEmpty(reply.PublicKey)
	require.NotEmpty(reply.ChainID)
}

func TestTFHEServiceGetCommittee(t *testing.T) {
	require := require.New(t)

	service := newTestTFHEService(t)

	args := &GetCommitteeArgs{}
	reply := &GetCommitteeReply{}

	err := service.GetCommittee(context.Background(), args, reply)
	require.NoError(err)

	require.Equal(uint64(1), reply.Epoch)
	require.Len(reply.Members, 2)
}

func TestTFHEServiceRegisterCiphertext(t *testing.T) {
	require := require.New(t)

	service := newTestTFHEService(t)

	args := &RegisterCiphertextArgs{
		Handle:  "0102030405060708091011121314151617181920212223242526272829303132",
		Owner:   "0102030405060708091011121314151617181920",
		Type:    1,
		Level:   14,
		Size:    1024,
	}
	reply := &RegisterCiphertextReply{}

	err := service.RegisterCiphertext(context.Background(), args, reply)
	require.NoError(err)

	require.Equal(args.Handle, reply.Handle)
	require.Equal(uint64(1), reply.Epoch)
	require.NotZero(reply.RegisteredAt)
}

func TestTFHEServiceGetCiphertextMeta(t *testing.T) {
	require := require.New(t)

	service := newTestTFHEService(t)

	// First register a ciphertext
	handle := "0102030405060708091011121314151617181920212223242526272829303132"
	registerArgs := &RegisterCiphertextArgs{
		Handle:  handle,
		Owner:   "0102030405060708091011121314151617181920",
		Type:    1,
		Level:   14,
		Size:    1024,
	}
	registerReply := &RegisterCiphertextReply{}
	err := service.RegisterCiphertext(context.Background(), registerArgs, registerReply)
	require.NoError(err)

	// Get the metadata
	getArgs := &GetCiphertextMetaArgs{
		Handle: handle,
	}
	getReply := &GetCiphertextMetaReply{}

	err = service.GetCiphertextMeta(context.Background(), getArgs, getReply)
	require.NoError(err)

	require.Equal(handle, getReply.Handle)
	require.Equal(uint8(1), getReply.Type)
	require.Equal(uint32(1024), getReply.Size)
}

func TestTFHEServiceRequestDecrypt(t *testing.T) {
	require := require.New(t)

	service := newTestTFHEService(t)

	// First register a ciphertext
	handle := "0102030405060708091011121314151617181920212223242526272829303132"
	registerArgs := &RegisterCiphertextArgs{
		Handle:  handle,
		Owner:   "0102030405060708091011121314151617181920",
		Type:    1,
		Level:   14,
		Size:    1024,
	}
	registerReply := &RegisterCiphertextReply{}
	err := service.RegisterCiphertext(context.Background(), registerArgs, registerReply)
	require.NoError(err)

	// First create a permit so we can decrypt
	permitArgs := &CreatePermitArgs{
		Handle:     handle,
		Grantor:    "0102030405060708091011121314151617181920",
		Grantee:    "abcdef0123456789abcdef0123456789abcdef01",
		Operations: 1, // decrypt
		Expiry:     time.Now().Add(time.Hour).Unix(),
	}
	permitReply := &CreatePermitReply{}
	err = service.CreatePermit(context.Background(), permitArgs, permitReply)
	require.NoError(err)

	// Request decryption
	args := &RequestDecryptArgs{
		CiphertextHandle: handle,
		PermitID:         permitReply.PermitID,
		Callback:         "abcdef0123456789abcdef0123456789abcdef01",
		CallbackSelector: "12345678",
	}
	reply := &RequestDecryptReply{}

	err = service.RequestDecrypt(context.Background(), args, reply)
	require.NoError(err)

	require.NotEmpty(reply.RequestID)
	require.Equal("pending", reply.Status)
	require.Equal(uint64(1), reply.Epoch)
}

func TestTFHEServiceGetDecryptResult(t *testing.T) {
	require := require.New(t)

	service := newTestTFHEService(t)

	// Register ciphertext
	handle := "0102030405060708091011121314151617181920212223242526272829303132"
	registerArgs := &RegisterCiphertextArgs{
		Handle:  handle,
		Owner:   "0102030405060708091011121314151617181920",
		Type:    1,
		Level:   14,
		Size:    1024,
	}
	err := service.RegisterCiphertext(context.Background(), registerArgs, &RegisterCiphertextReply{})
	require.NoError(err)

	// Create permit
	permitArgs := &CreatePermitArgs{
		Handle:     handle,
		Grantor:    "0102030405060708091011121314151617181920",
		Grantee:    "abcdef0123456789abcdef0123456789abcdef01",
		Operations: 1, // decrypt
		Expiry:     time.Now().Add(time.Hour).Unix(),
	}
	permitReply := &CreatePermitReply{}
	err = service.CreatePermit(context.Background(), permitArgs, permitReply)
	require.NoError(err)

	// Request decryption
	requestArgs := &RequestDecryptArgs{
		CiphertextHandle: handle,
		PermitID:         permitReply.PermitID,
		Callback:         "abcdef0123456789abcdef0123456789abcdef01",
		CallbackSelector: "12345678",
	}
	requestReply := &RequestDecryptReply{}
	err = service.RequestDecrypt(context.Background(), requestArgs, requestReply)
	require.NoError(err)

	// Get result (should be pending)
	args := &GetDecryptResultArgs{
		RequestID: requestReply.RequestID,
	}
	reply := &GetDecryptResultReply{}

	err = service.GetDecryptResult(context.Background(), args, reply)
	require.NoError(err)

	require.Equal(requestReply.RequestID, reply.RequestID)
	require.Equal("pending", reply.Status)
}

func TestTFHEServiceCreatePermit(t *testing.T) {
	require := require.New(t)

	service := newTestTFHEService(t)

	// First register a ciphertext
	handle := "0102030405060708091011121314151617181920212223242526272829303132"
	registerArgs := &RegisterCiphertextArgs{
		Handle:  handle,
		Owner:   "0102030405060708091011121314151617181920",
		Type:    1,
		Level:   14,
		Size:    1024,
	}
	err := service.RegisterCiphertext(context.Background(), registerArgs, &RegisterCiphertextReply{})
	require.NoError(err)

	args := &CreatePermitArgs{
		Handle:     handle,
		Grantor:    "0102030405060708091011121314151617181920",
		Grantee:    "abcdef0123456789abcdef0123456789abcdef01",
		Operations: 3, // decrypt + reencrypt
		Expiry:     time.Now().Add(time.Hour).Unix(),
	}
	reply := &CreatePermitReply{}

	err = service.CreatePermit(context.Background(), args, reply)
	require.NoError(err)

	require.NotEmpty(reply.PermitID)
	require.NotZero(reply.CreatedAt)
}

func TestTFHEServiceVerifyPermit(t *testing.T) {
	require := require.New(t)

	service := newTestTFHEService(t)

	handle := "0102030405060708091011121314151617181920212223242526272829303132"
	grantee := "abcdef0123456789abcdef0123456789abcdef01"

	// First register a ciphertext
	registerArgs := &RegisterCiphertextArgs{
		Handle:  handle,
		Owner:   "0102030405060708091011121314151617181920",
		Type:    1,
		Level:   14,
		Size:    1024,
	}
	err := service.RegisterCiphertext(context.Background(), registerArgs, &RegisterCiphertextReply{})
	require.NoError(err)

	// Create permit
	createArgs := &CreatePermitArgs{
		Handle:     handle,
		Grantor:    "0102030405060708091011121314151617181920",
		Grantee:    grantee,
		Operations: 1, // decrypt
		Expiry:     time.Now().Add(time.Hour).Unix(),
	}
	createReply := &CreatePermitReply{}
	err = service.CreatePermit(context.Background(), createArgs, createReply)
	require.NoError(err)

	// Verify permit
	verifyArgs := &VerifyPermitArgs{
		PermitID:  createReply.PermitID,
		Handle:    handle,
		Grantee:   grantee,
		Operation: 1, // decrypt
	}
	verifyReply := &VerifyPermitReply{}

	err = service.VerifyPermit(context.Background(), verifyArgs, verifyReply)
	require.NoError(err)

	require.True(verifyReply.Valid)
}

func TestTFHEServiceVerifyPermitInvalid(t *testing.T) {
	require := require.New(t)

	service := newTestTFHEService(t)

	// Verify non-existent permit
	verifyArgs := &VerifyPermitArgs{
		PermitID:  "0102030405060708091011121314151617181920212223242526272829303132",
		Handle:    "0102030405060708091011121314151617181920212223242526272829303132",
		Grantee:   "abcdef0123456789abcdef0123456789abcdef01",
		Operation: 1,
	}
	verifyReply := &VerifyPermitReply{}

	err := service.VerifyPermit(context.Background(), verifyArgs, verifyReply)
	require.NoError(err)

	require.False(verifyReply.Valid)
	require.NotEmpty(verifyReply.Error)
}

func TestTFHEServiceNotInitialized(t *testing.T) {
	require := require.New(t)

	service := &TFHEService{
		logger:  log.NewNoOpLogger(),
		chainID: ids.GenerateTestID(),
		// registry is nil
	}

	err := service.GetPublicParams(context.Background(), &GetPublicParamsArgs{}, &GetPublicParamsReply{})
	require.Error(err)
	require.Equal(ErrNotInitialized, err)
}

func TestTFHEServiceInvalidHandleFormat(t *testing.T) {
	require := require.New(t)

	service := newTestTFHEService(t)

	// Invalid hex
	args := &RegisterCiphertextArgs{
		Handle:  "not-valid-hex",
		Owner:   "0102030405060708091011121314151617181920",
		Type:    1,
		Level:   14,
		Size:    1024,
	}
	reply := &RegisterCiphertextReply{}

	err := service.RegisterCiphertext(context.Background(), args, reply)
	require.Error(err)

	// Wrong length
	args.Handle = "0102030405" // Too short
	err = service.RegisterCiphertext(context.Background(), args, reply)
	require.Error(err)
}
