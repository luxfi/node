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
)

// TestParseBlock_MalformedBlockID_IsTypedAndQuarantined is the guard on the
// ParseBlock response boundary: an id the plugin never filled in must be
// refused with a typed error, not surfaced as an opaque codec complaint.
//
// THE SYMPTOM IT PREVENTS: a validator that cannot vote, reporting only
//
//	warn ParseBlock failed, cannot vote correctly
//	     error="invalid hash length: expected 32 bytes but got 0"
//
// THE MECHANISM: the C-Chain EVM runs as an rpcchainvm plugin across
// the ZAP boundary. A node sitting on a divergent, already-accepted fork block,
// asked via PushQuery to ParseBlock a CANONICAL block that does not connect to
// its chain, is answered with a BlockResponse whose ID/ParentID are EMPTY
// (0-length) and whose Err is left at ErrorUnspecified. A client that passes
// that empty slice straight into ids.ToID turns it into the opaque "invalid
// hash length: expected 32 bytes but got 0" — a complaint about a field width,
// which masks the real "does-not-connect" condition and points diagnosis at the
// codec instead of at the divergence.
//
// The wire codec (github.com/luxfi/api/zap BlockResponse) genuinely admits a
// 0-length ID/ParentID: both are length-prefixed []byte read via ReadBytes, and
// Err defaults to ErrorUnspecified, so an empty-id response slips past the
// `resp.Err != ErrorUnspecified` guard. This test reproduces that exact wire
// shape (not a hand-fabricated convenience) and asserts the hardened behavior:
//
//   - a 0-length (or any non-32-byte) id yields the explicit typed
//     errMalformedBlockID, NOT the opaque ids.ToID error;
//   - ParseBlock returns a nil block (no state advance, no zero-id coercion);
//   - ParseBlock never panics on the malformed field;
//   - a well-formed 32-byte response — INCLUDING a 32-zero-byte ParentID, which
//     is the legitimate ids.Empty value and must NOT be confused with the
//     0-length malformed case — still parses cleanly.
//
// Run under -race.
func TestParseBlock_MalformedBlockID_IsTypedAndQuarantined(t *testing.T) {
	goodID := ids.ID{0x11, 0x22, 0x33}     // a valid, non-empty 32-byte id
	goodParent := ids.ID{0xaa, 0xbb, 0xcc} // a valid, non-empty 32-byte parent
	zeroParent := ids.Empty                // 32 ZERO bytes — a legitimate id value

	cases := []struct {
		name    string
		resp    *zapwire.BlockResponse
		wantErr bool
	}{
		{
			// The wire shape a diverged plugin answers with: empty id, no error code.
			name:    "empty id with no error code — reads as success and wedges the caller",
			resp:    &zapwire.BlockResponse{ID: nil, ParentID: goodParent[:], Bytes: []byte{0xde, 0xad}, Err: zapwire.ErrorUnspecified},
			wantErr: true,
		},
		{
			name:    "empty parentID with no error code",
			resp:    &zapwire.BlockResponse{ID: goodID[:], ParentID: nil, Bytes: []byte{0xde, 0xad}, Err: zapwire.ErrorUnspecified},
			wantErr: true,
		},
		{
			// Malleability: any non-32 length must be rejected, not truncated/padded.
			name:    "short 5-byte id is rejected (length malleability guard)",
			resp:    &zapwire.BlockResponse{ID: []byte{1, 2, 3, 4, 5}, ParentID: goodParent[:], Bytes: []byte{0xde, 0xad}, Err: zapwire.ErrorUnspecified},
			wantErr: true,
		},
		{
			// Well-formed: 32-byte id + 32-ZERO-byte parent (legit ids.Empty) parses.
			name:    "well-formed 32-byte ids, zero-but-32-byte parent parses cleanly",
			resp:    &zapwire.BlockResponse{ID: goodID[:], ParentID: zeroParent[:], Bytes: []byte{0xde, 0xad}, Height: 7, Timestamp: time.Now().UnixNano(), Err: zapwire.ErrorUnspecified},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c := newParseBlockTestClient(t, tc.resp)

			var (
				blk block.Block
				err error
			)
			// Must never panic regardless of the malformed field.
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("ParseBlock panicked on malformed response: %v", r)
					}
				}()
				blk, err = c.ParseBlock(context.Background(), []byte{0xde, 0xad})
			}()

			if tc.wantErr {
				if !errors.Is(err, errMalformedBlockID) {
					t.Fatalf("want typed errMalformedBlockID, got %v", err)
				}
				if blk != nil {
					t.Fatalf("malformed id must yield a nil block (no state advance / no zero-id coercion), got %v", blk)
				}
				return
			}
			if err != nil {
				t.Fatalf("well-formed response must parse, got err: %v", err)
			}
			if blk == nil {
				t.Fatal("well-formed response must yield a non-nil block")
			}
			if blk.ID() != goodID {
				t.Fatalf("block id = %s, want %s", blk.ID(), goodID)
			}
			if blk.Parent() != zeroParent {
				t.Fatalf("parent id = %s, want %s (the legitimate 32-byte zero id)", blk.Parent(), zeroParent)
			}
		})
	}
}

// newParseBlockTestClient spins an in-process ZAP server whose MsgParseBlock
// handler returns the supplied BlockResponse verbatim — modelling exactly what
// a VM plugin over rpcchainvm puts on the wire — and returns a Client
// connected to it.
func newParseBlockTestClient(t *testing.T, resp *zapwire.BlockResponse) *Client {
	t.Helper()
	addr, stop := startTestServer(t, zapwire.HandlerFunc(func(_ context.Context, msgType zapwire.MessageType, _ []byte) (zapwire.MessageType, []byte, error) {
		if msgType != zapwire.MsgParseBlock {
			return 0, nil, errors.New("unexpected message")
		}
		buf := zapwire.GetBuffer()
		defer zapwire.PutBuffer(buf)
		resp.Encode(buf)
		out := make([]byte, len(buf.Bytes()))
		copy(out, buf.Bytes())
		return zapwire.MsgParseBlock, out, nil
	}))
	t.Cleanup(stop)

	conn, err := zapwire.Dial(context.Background(), addr, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return NewClient(conn, log.NewNoOpLogger())
}
