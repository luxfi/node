// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	luxruntime "github.com/luxfi/runtime"
)

// TestInitialize_SuccessResponseNotMistakenForError is a regression guard
// for a wire-protocol misread that bricked every C-Chain start in a
// downstream EVM L1 stack: the client treated every response with
// MsgResponseFlag set (i.e. ALL responses) as an error, then printed the
// binary InitializeResponse payload as if it were the error string. The
// result was "vm error: <unprintable bytes>" and a chain that never came up
// despite the plugin reporting "VM initialized successfully".
//
// What this test asserts: when the plugin returns a well-formed
// InitializeResponse (with MsgResponseFlag — the normal success encoding),
// the client decodes it instead of synthesizing a spurious error. The
// payload echoes back a known LastAcceptedID so we can confirm the client
// actually parsed the response rather than just not erroring.
//
// Why this matters: the transport already converts true error responses
// (MsgErrorFlag set) into a non-nil err from Conn.Call, so the client's
// only job after Call returns success is to decode the response. The
// previous code's defense against "error responses" was both wrong AND
// unreachable — defense against a case that can't happen, that fired
// every time anyway.
func TestInitialize_SuccessResponseNotMistakenForError(t *testing.T) {
	wantLastAccepted := ids.ID{0xa1, 0xcc, 0xa4, 0xae, 0xb9, 0x08, 0x14, 0x8c}
	wantParent := ids.ID{}

	addr, stop := startTestServer(t, zapwire.HandlerFunc(func(_ context.Context, msgType zapwire.MessageType, _ []byte) (zapwire.MessageType, []byte, error) {
		if msgType != zapwire.MsgInitialize {
			return 0, nil, errors.New("unexpected message")
		}
		resp := &zapwire.InitializeResponse{
			LastAcceptedID:       wantLastAccepted[:],
			LastAcceptedParentID: wantParent[:],
			Height:               0,
			Bytes:                []byte{},
			Timestamp:            time.Now().UnixNano(),
		}
		buf := zapwire.GetBuffer()
		defer zapwire.PutBuffer(buf)
		resp.Encode(buf)
		out := make([]byte, len(buf.Bytes()))
		copy(out, buf.Bytes())
		return zapwire.MsgInitialize, out, nil
	}))
	defer stop()

	conn, err := zapwire.Dial(context.Background(), addr, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	c := NewClient(conn, log.NewNoOpLogger())
	rt := &luxruntime.Runtime{NetworkID: 1337}
	if err := c.Initialize(context.Background(), block.Init{
		Runtime: rt,
		Genesis: []byte("{}"),
	}); err != nil {
		t.Fatalf("Initialize returned error on success response: %v", err)
	}
	if c.lastAcceptedID != wantLastAccepted {
		t.Fatalf("lastAcceptedID not parsed: got %s want %s",
			c.lastAcceptedID, wantLastAccepted)
	}
}

// TestInitialize_ErrorResponseSurfacedAsError confirms the OTHER half of
// the contract: when the plugin actually returns an error, the client
// reports it as an error (rather than swallowing it as a success). The
// transport translates an error response into a non-nil err out of
// Conn.Call, so the client's wrapping of that err is what we check here.
func TestInitialize_ErrorResponseSurfacedAsError(t *testing.T) {
	addr, stop := startTestServer(t, zapwire.HandlerFunc(func(_ context.Context, _ zapwire.MessageType, _ []byte) (zapwire.MessageType, []byte, error) {
		return 0, nil, errors.New("synthesized init failure")
	}))
	defer stop()

	conn, err := zapwire.Dial(context.Background(), addr, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	c := NewClient(conn, log.NewNoOpLogger())
	rt := &luxruntime.Runtime{NetworkID: 1337}
	gotErr := c.Initialize(context.Background(), block.Init{
		Runtime: rt,
		Genesis: []byte("{}"),
	})
	if gotErr == nil {
		t.Fatalf("Initialize swallowed server error and returned nil")
	}
	if !strings.Contains(gotErr.Error(), "synthesized init failure") {
		t.Fatalf("error from server not surfaced; got %q", gotErr.Error())
	}
}

// startTestServer starts an in-process ZAP server on loopback and returns
// the dial-able address plus a teardown closure. Using net.Listen on
// 127.0.0.1:0 lets the OS pick a free port — no shared state across tests.
func startTestServer(t *testing.T, handler zapwire.Handler) (string, func()) {
	t.Helper()

	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	listener := zapwire.NewListener(raw, nil)
	srv := zapwire.NewServer(listener, handler)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Serve(ctx) }()

	return raw.Addr().String(), func() {
		cancel()
		_ = listener.Close()
	}
}
