// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zwarp

import (
	"context"
	"net"
	"testing"
	"time"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/warp"
)

// mockSigner implements warp.Signer for testing
type mockSigner struct {
	signature []byte
	publicKey []byte
	signErr   error
}

func (m *mockSigner) Sign(msg *warp.UnsignedMessage) ([]byte, error) {
	if m.signErr != nil {
		return nil, m.signErr
	}
	return m.signature, nil
}

func (m *mockSigner) PublicKey() []byte {
	return m.publicKey
}

func TestZWarpClientServer(t *testing.T) {
	// Create a mock signer
	expectedSig := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	signer := &mockSigner{
		signature: expectedSig,
		publicKey: []byte{9, 10, 11, 12},
	}

	// Create server to verify it compiles correctly
	server := NewServer(signer)
	_ = server.Handler() // Verify Handler method works

	// Create a pipe connection for testing
	_, clientConn := net.Pipe()
	defer clientConn.Close()

	// Wrap in ZAP connection
	zapClientConn := zapwire.NewConn(clientConn, nil)
	defer zapClientConn.Close()

	// Create client
	client := NewClient(zapClientConn)

	// Test signing
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msg, err := warp.NewUnsignedMessage(1, ids.GenerateTestID(), []byte("test payload"))
	if err != nil {
		t.Fatalf("Failed to create unsigned message: %v", err)
	}

	// Test that the client implements warp.Signer
	var _ warp.Signer = client

	// Since we're using a pipe without a proper server loop, just verify the client is created
	_ = ctx
	_ = msg
	t.Log("ZWarp client/server types verified")
}

func TestServerHandleMessage(t *testing.T) {
	expectedSig := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	signer := &mockSigner{
		signature: expectedSig,
		publicKey: []byte{9, 10, 11, 12},
	}

	server := NewServer(signer)

	// Test sign request
	sourceChainID := ids.GenerateTestID()
	req := &zapwire.WarpSignRequest{
		NetworkID:     1,
		SourceChainID: sourceChainID[:],
		Payload:       []byte("test payload"),
	}

	buf := zapwire.GetBuffer()
	req.Encode(buf)

	ctx := context.Background()
	respPayload, err := server.HandleMessage(ctx, zapwire.MsgWarpSign, buf.Bytes())
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	zapwire.PutBuffer(buf)

	// Decode response
	resp := &zapwire.WarpSignResponse{}
	if err := resp.Decode(zapwire.NewReader(respPayload)); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Error != "" {
		t.Fatalf("Unexpected error in response: %s", resp.Error)
	}

	if string(resp.Signature) != string(expectedSig) {
		t.Errorf("Signature mismatch: got %v, want %v", resp.Signature, expectedSig)
	}
}

func TestServerHandleGetPublicKey(t *testing.T) {
	expectedPubKey := []byte{9, 10, 11, 12}
	signer := &mockSigner{
		publicKey: expectedPubKey,
	}

	server := NewServer(signer)

	ctx := context.Background()
	respPayload, err := server.HandleMessage(ctx, zapwire.MsgWarpGetPublicKey, nil)
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	// Decode response
	resp := &zapwire.WarpGetPublicKeyResponse{}
	if err := resp.Decode(zapwire.NewReader(respPayload)); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Error != "" {
		t.Fatalf("Unexpected error in response: %s", resp.Error)
	}

	if string(resp.PublicKey) != string(expectedPubKey) {
		t.Errorf("PublicKey mismatch: got %v, want %v", resp.PublicKey, expectedPubKey)
	}
}

func TestServerHandleBatchSign(t *testing.T) {
	expectedSig := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	signer := &mockSigner{
		signature: expectedSig,
	}

	server := NewServer(signer)

	// Create batch request
	sourceChainID1 := ids.GenerateTestID()
	sourceChainID2 := ids.GenerateTestID()

	req := &zapwire.WarpBatchSignRequest{
		Messages: []zapwire.WarpSignRequest{
			{
				NetworkID:     1,
				SourceChainID: sourceChainID1[:],
				Payload:       []byte("payload1"),
			},
			{
				NetworkID:     1,
				SourceChainID: sourceChainID2[:],
				Payload:       []byte("payload2"),
			},
		},
	}

	buf := zapwire.GetBuffer()
	req.Encode(buf)

	ctx := context.Background()
	respPayload, err := server.HandleMessage(ctx, zapwire.MsgWarpBatchSign, buf.Bytes())
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	zapwire.PutBuffer(buf)

	// Decode response
	resp := &zapwire.WarpBatchSignResponse{}
	if err := resp.Decode(zapwire.NewReader(respPayload)); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.Signatures) != 2 {
		t.Fatalf("Expected 2 signatures, got %d", len(resp.Signatures))
	}

	for i, sig := range resp.Signatures {
		if resp.Errors[i] != "" {
			t.Errorf("Unexpected error for signature %d: %s", i, resp.Errors[i])
		}
		if string(sig) != string(expectedSig) {
			t.Errorf("Signature %d mismatch: got %v, want %v", i, sig, expectedSig)
		}
	}
}

func TestServerHandleUnknownMessageType(t *testing.T) {
	signer := &mockSigner{}
	server := NewServer(signer)

	ctx := context.Background()
	_, err := server.HandleMessage(ctx, zapwire.MessageType(255), nil)
	if err == nil {
		t.Fatal("Expected error for unknown message type")
	}
}
