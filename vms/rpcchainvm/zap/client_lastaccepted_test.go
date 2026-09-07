// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"context"
	"errors"
	"testing"
	"time"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	luxruntime "github.com/luxfi/runtime"
)

// TestLastAccepted_RefreshedOnAccept: the ZAP client's LastAccepted() must reflect the
// most-recently-ACCEPTED block, not a frozen Initialize snapshot.
//
// zapBlock.Accept fires the MsgBlockAccept wire call; if it does not also refresh
// c.lastAcceptedID, that field keeps whatever Initialize wrote and LastAccepted() returns the
// boot id for the process life while the plugin's own store advances. Every consumer of
// VM.LastAccepted is then misled: GetAcceptedFrontier serves a stale tip to peers, and any
// caught-up or height signal read from it re-descends forever.
//
// This drives a real in-process ZAP server: Initialize seeds the cache, a successful Accept must
// refresh it to the accepted id, and a FAILED Accept must leave it unchanged (a failed accept did
// not advance the plugin).
func TestLastAccepted_RefreshedOnAccept(t *testing.T) {
	seedID := ids.ID{0x5e, 0xed}
	acceptedID := ids.ID{0xac, 0xce, 0x97, 0xed}
	failID := ids.ID{0xfa, 0x11}

	var failAccept bool
	addr, stop := startTestServer(t, zapwire.HandlerFunc(func(_ context.Context, msgType zapwire.MessageType, _ []byte) (zapwire.MessageType, []byte, error) {
		switch msgType {
		case zapwire.MsgInitialize:
			var zeroParent ids.ID
			resp := &zapwire.InitializeResponse{
				LastAcceptedID:       seedID[:],
				LastAcceptedParentID: zeroParent[:],
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
		case zapwire.MsgBlockAccept:
			if failAccept {
				return 0, nil, errors.New("synthesized accept failure")
			}
			return zapwire.MsgBlockAccept, []byte{}, nil
		default:
			return 0, nil, errors.New("unexpected message")
		}
	}))
	defer stop()

	conn, err := zapwire.Dial(context.Background(), addr, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	c := NewClient(conn, log.NewNoOpLogger())
	if err := c.Initialize(context.Background(), block.Init{
		Runtime: &luxruntime.Runtime{NetworkID: 1337},
		Genesis: []byte("{}"),
	}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Seeded at Initialize.
	if got, _ := c.LastAccepted(context.Background()); got != seedID {
		t.Fatalf("after Initialize: LastAccepted = %s, want seed %s", got, seedID)
	}

	// A successful Accept REFRESHES the cache (the fix — previously it stayed frozen at seed).
	if err := (&zapBlock{client: c, id: acceptedID}).Accept(context.Background()); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if got, _ := c.LastAccepted(context.Background()); got != acceptedID {
		t.Fatalf("after Accept: LastAccepted = %s, want accepted %s (cache did not refresh — the freeze)", got, acceptedID)
	}

	// A FAILED Accept must NOT move the cache (the plugin did not advance).
	failAccept = true
	if err := (&zapBlock{client: c, id: failID}).Accept(context.Background()); err == nil {
		t.Fatal("expected the synthesized accept failure to surface")
	}
	if got, _ := c.LastAccepted(context.Background()); got != acceptedID {
		t.Fatalf("after FAILED Accept: LastAccepted = %s, want unchanged %s", got, acceptedID)
	}
}
