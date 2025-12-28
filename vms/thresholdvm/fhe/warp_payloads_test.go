// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fhe

import (
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/stretchr/testify/require"
)

func TestFHEDecryptRequestV1BytesAndParse(t *testing.T) {
	require := require.New(t)

	var requestID [32]byte
	copy(requestID[:], []byte("request-id-12345678901234567"))

	var ctHandle [32]byte
	copy(ctHandle[:], []byte("ciphertext-handle-1234567890"))

	var permitID [32]byte
	copy(permitID[:], []byte("permit-id-12345678901234567"))

	var requester [20]byte
	copy(requester[:], []byte("requester12345678"))

	var callback [20]byte
	copy(callback[:], []byte("callback-address12"))

	var selector [4]byte
	copy(selector[:], []byte{0xAB, 0xCD, 0xEF, 0x12})

	request := &FHEDecryptRequestV1{
		RequestID:        requestID,
		CiphertextHandle: ctHandle,
		PermitID:         permitID,
		SourceChainID:    ids.GenerateTestID(),
		Epoch:            1,
		Nonce:            100,
		Expiry:           time.Now().Add(time.Hour).Unix(),
		Requester:        requester,
		Callback:         callback,
		CallbackSelector: selector,
		GasLimit:         1000000,
	}

	// Serialize
	data := request.Bytes()
	require.Len(data, 202)

	// Parse back
	parsed, err := ParseFHEDecryptRequestV1(data)
	require.NoError(err)

	require.Equal(request.RequestID, parsed.RequestID)
	require.Equal(request.CiphertextHandle, parsed.CiphertextHandle)
	require.Equal(request.PermitID, parsed.PermitID)
	require.Equal(request.SourceChainID, parsed.SourceChainID)
	require.Equal(request.Epoch, parsed.Epoch)
	require.Equal(request.Nonce, parsed.Nonce)
	require.Equal(request.Expiry, parsed.Expiry)
	require.Equal(request.Requester, parsed.Requester)
	require.Equal(request.Callback, parsed.Callback)
	require.Equal(request.CallbackSelector, parsed.CallbackSelector)
	require.Equal(request.GasLimit, parsed.GasLimit)
}

func TestFHEDecryptResultV1BytesAndParse(t *testing.T) {
	require := require.New(t)

	var requestID [32]byte
	copy(requestID[:], []byte("request-id-12345678901234567"))

	var resultHandle [32]byte
	copy(resultHandle[:], []byte("result-handle-12345678901234"))

	var signature [32]byte
	copy(signature[:], []byte("committee-signature-12345678"))

	result := &FHEDecryptResultV1{
		RequestID:          requestID,
		ResultHandle:       resultHandle,
		SourceChainID:      ids.GenerateTestID(),
		Epoch:              1,
		Status:             DecryptStatusSuccess,
		CommitteeSignature: signature,
		Plaintext:          []byte("decrypted-plaintext-data"),
	}

	// Serialize
	data := result.Bytes()
	require.NotEmpty(data)

	// Parse back
	parsed, err := ParseFHEDecryptResultV1(data)
	require.NoError(err)

	require.Equal(result.RequestID, parsed.RequestID)
	require.Equal(result.ResultHandle, parsed.ResultHandle)
	require.Equal(result.SourceChainID, parsed.SourceChainID)
	require.Equal(result.Epoch, parsed.Epoch)
	require.Equal(result.Status, parsed.Status)
	require.Equal(result.CommitteeSignature, parsed.CommitteeSignature)
	require.Equal(result.Plaintext, parsed.Plaintext)
}

func TestFHEDecryptResultV1Failed(t *testing.T) {
	require := require.New(t)

	var requestID [32]byte
	copy(requestID[:], []byte("request-id-12345678901234567"))

	result := &FHEDecryptResultV1{
		RequestID:     requestID,
		SourceChainID: ids.GenerateTestID(),
		Epoch:         1,
		Status:        DecryptStatusFailed,
		Plaintext:     nil,
	}

	data := result.Bytes()
	parsed, err := ParseFHEDecryptResultV1(data)
	require.NoError(err)

	require.Equal(DecryptStatusFailed, parsed.Status)
	require.Empty(parsed.Plaintext)
}

func TestFHEReencryptRequestV1BytesAndParse(t *testing.T) {
	require := require.New(t)

	var requestID [32]byte
	copy(requestID[:], []byte("request-id-12345678901234567"))

	var ctHandle [32]byte
	copy(ctHandle[:], []byte("ciphertext-handle-1234567890"))

	var permitID [32]byte
	copy(permitID[:], []byte("permit-id-12345678901234567"))

	var recipient [20]byte
	copy(recipient[:], []byte("recipient12345678"))

	request := &FHEReencryptRequestV1{
		RequestID:          requestID,
		CiphertextHandle:   ctHandle,
		PermitID:           permitID,
		SourceChainID:      ids.GenerateTestID(),
		Epoch:              1,
		Recipient:          recipient,
		RecipientPublicKey: []byte("recipient-public-key-data-here"),
	}

	// Serialize
	data := request.Bytes()
	require.NotEmpty(data)

	// Parse back
	parsed, err := ParseFHEReencryptRequestV1(data)
	require.NoError(err)

	require.Equal(request.RequestID, parsed.RequestID)
	require.Equal(request.CiphertextHandle, parsed.CiphertextHandle)
	require.Equal(request.PermitID, parsed.PermitID)
	require.Equal(request.SourceChainID, parsed.SourceChainID)
	require.Equal(request.Epoch, parsed.Epoch)
	require.Equal(request.Recipient, parsed.Recipient)
	require.Equal(request.RecipientPublicKey, parsed.RecipientPublicKey)
}

func TestFHETaskResultV1BytesAndParse(t *testing.T) {
	require := require.New(t)

	var taskID [32]byte
	copy(taskID[:], []byte("task-id-123456789012345678901"))

	var resultHandle [32]byte
	copy(resultHandle[:], []byte("result-handle-12345678901234"))

	var callback [20]byte
	copy(callback[:], []byte("callback-address12"))

	var selector [4]byte
	copy(selector[:], []byte{0xAB, 0xCD, 0xEF, 0x12})

	var signature [32]byte
	copy(signature[:], []byte("signature-data-12345678901234"))

	result := &FHETaskResultV1{
		TaskID:           taskID,
		ResultHandle:     resultHandle,
		SourceChainID:    ids.GenerateTestID(),
		Epoch:            1,
		Status:           TaskStatusCompleted,
		Callback:         callback,
		CallbackSelector: selector,
		Signature:        signature,
	}

	// Serialize
	data := result.Bytes()
	require.Len(data, 163)

	// Parse back
	parsed, err := ParseFHETaskResultV1(data)
	require.NoError(err)

	require.Equal(result.TaskID, parsed.TaskID)
	require.Equal(result.ResultHandle, parsed.ResultHandle)
	require.Equal(result.SourceChainID, parsed.SourceChainID)
	require.Equal(result.Epoch, parsed.Epoch)
	require.Equal(result.Status, parsed.Status)
	require.Equal(result.Callback, parsed.Callback)
	require.Equal(result.CallbackSelector, parsed.CallbackSelector)
	require.Equal(result.Signature, parsed.Signature)
}

func TestDecryptStatusConstants(t *testing.T) {
	require := require.New(t)

	require.Equal(uint8(0x00), DecryptStatusSuccess)
	require.Equal(uint8(0x01), DecryptStatusFailed)
	require.Equal(uint8(0x02), DecryptStatusExpired)
	require.Equal(uint8(0x03), DecryptStatusDenied)
}

func TestTaskStatusConstants(t *testing.T) {
	require := require.New(t)

	require.Equal(uint8(0x00), TaskStatusCompleted)
	require.Equal(uint8(0x01), TaskStatusFailed)
	require.Equal(uint8(0x02), TaskStatusTimeout)
}

func TestPayloadVersionAndTypeConstants(t *testing.T) {
	require := require.New(t)

	require.Equal(uint8(0x01), PayloadVersionV1)
	require.Equal(uint8(0x01), PayloadTypeFHEDecryptRequestV1)
	require.Equal(uint8(0x02), PayloadTypeFHEDecryptResultV1)
	require.Equal(uint8(0x03), PayloadTypeFHEReencryptRequestV1)
	require.Equal(uint8(0x04), PayloadTypeFHETaskResultV1)
	require.Equal(uint8(0x05), PayloadTypeFHEKeyRotationV1)
}

func TestParseInvalidPayload(t *testing.T) {
	require := require.New(t)

	// Too short
	_, err := ParseFHEDecryptRequestV1([]byte{0x01, 0x02})
	require.Error(err)

	// Invalid version
	data := make([]byte, 202)
	data[0] = 0xFF
	_, err = ParseFHEDecryptRequestV1(data)
	require.Error(err)

	// Invalid type
	data[0] = PayloadVersionV1
	data[1] = 0xFF
	_, err = ParseFHEDecryptRequestV1(data)
	require.Error(err)
}
