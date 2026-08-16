// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/vm/chain"
)

// record is what a peer was asked. Read through syncPeer.seen(), which takes the
// lock, so a test reads it without racing the goroutine serving the connection.
type record struct {
	asked    []zapwire.MessageType
	parsed   []byte
	height   uint64
	accepted []byte
}

// syncPeer is a fake plugin holding the far side of the state-sync wire. It
// answers the six questions from what a test scripted and records what it was
// asked, so a test can read both halves of an exchange.
type syncPeer struct {
	enabled zapwire.StateSyncEnabledResponse
	summary zapwire.SummaryResponse
	accept  zapwire.StateSummaryAcceptResponse

	mu  sync.Mutex
	got record
}

func (p *syncPeer) seen() record {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.got
}

func (p *syncPeer) handle(_ context.Context, op zapwire.MessageType, payload []byte) (zapwire.MessageType, []byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got.asked = append(p.got.asked, op)

	var resp encoder
	switch op {
	case zapwire.MsgStateSyncEnabled:
		resp = &p.enabled
	case zapwire.MsgGetOngoingSyncStateSummary, zapwire.MsgGetLastStateSummary:
		resp = &p.summary
	case zapwire.MsgParseStateSummary:
		req := &zapwire.ParseStateSummaryRequest{}
		if err := req.Decode(zapwire.NewReader(payload)); err != nil {
			return 0, nil, err
		}
		p.got.parsed = req.Bytes
		resp = &p.summary
	case zapwire.MsgGetStateSummary:
		req := &zapwire.GetStateSummaryRequest{}
		if err := req.Decode(zapwire.NewReader(payload)); err != nil {
			return 0, nil, err
		}
		p.got.height = req.Height
		resp = &p.summary
	case zapwire.MsgStateSummaryAccept:
		req := &zapwire.StateSummaryAcceptRequest{}
		if err := req.Decode(zapwire.NewReader(payload)); err != nil {
			return 0, nil, err
		}
		p.got.accepted = req.ID
		resp = &p.accept
	default:
		return 0, nil, fmt.Errorf("peer was asked a question it does not answer: %d", op)
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	resp.Encode(buf)
	out := make([]byte, len(buf.Bytes()))
	copy(out, buf.Bytes())
	return op, out, nil
}

// connect stands a handler up as an in-process peer and returns a Client talking
// to it over a real ZAP connection.
func connect(t *testing.T, handler zapwire.Handler) *Client {
	t.Helper()

	addr, stop := startTestServer(t, handler)
	t.Cleanup(stop)

	conn, err := zapwire.Dial(context.Background(), addr, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return NewClient(conn, log.NewNoOpLogger())
}

// TestClientOffersTheStateSyncSurface makes the same assertion the wrapper VM
// makes on the VM it wraps. A Client that fails it leaves every plugin-hosted
// chain unable to sync state, whatever the plugin behind it can do — and it
// fails silently there, so it is asserted here.
func TestClientOffersTheStateSyncSurface(t *testing.T) {
	var vm any = (*Client)(nil)
	if _, ok := vm.(chain.StateSyncableVM); !ok {
		t.Fatal("*Client does not carry the state-sync surface")
	}
}

// TestSummaryCarriesWhatTheReplyNamed covers the four questions that answer with
// a summary: each asks its own question, carries its own request field, and
// hands back a summary reading exactly what the reply named.
func TestSummaryCarriesWhatTheReplyNamed(t *testing.T) {
	want := ids.ID{0x5a, 0x11, 0x0c}
	wantBytes := []byte{0xc0, 0xff, 0xee}
	const wantHeight = 4291

	peer := &syncPeer{summary: zapwire.SummaryResponse{
		ID:     want[:],
		Height: wantHeight,
		Bytes:  wantBytes,
	}}
	c := connect(t, zapwire.HandlerFunc(peer.handle))
	ctx := context.Background()

	cases := []struct {
		name string
		op   zapwire.MessageType
		ask  func() (chain.StateSummary, error)
	}{
		{"ongoing", zapwire.MsgGetOngoingSyncStateSummary, func() (chain.StateSummary, error) {
			return c.GetOngoingSyncStateSummary(ctx)
		}},
		{"last", zapwire.MsgGetLastStateSummary, func() (chain.StateSummary, error) {
			return c.GetLastStateSummary(ctx)
		}},
		{"parsed", zapwire.MsgParseStateSummary, func() (chain.StateSummary, error) {
			return c.ParseStateSummary(ctx, []byte{0x01, 0x02, 0x03})
		}},
		{"at height", zapwire.MsgGetStateSummary, func() (chain.StateSummary, error) {
			return c.GetStateSummary(ctx, wantHeight)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := tc.ask()
			if err != nil {
				t.Fatalf("asking for a summary the peer holds failed: %v", err)
			}
			if asked := peer.seen().asked; asked[len(asked)-1] != tc.op {
				t.Fatalf("peer was asked %d, want %d", asked[len(asked)-1], tc.op)
			}
			if s.ID() != want {
				t.Fatalf("summary id = %s, want %s", s.ID(), want)
			}
			if s.Height() != wantHeight {
				t.Fatalf("summary height = %d, want %d", s.Height(), wantHeight)
			}
			if !bytes.Equal(s.Bytes(), wantBytes) {
				t.Fatalf("summary bytes = %x, want %x", s.Bytes(), wantBytes)
			}
		})
	}

	// The two questions that carry a field carried the caller's, not a default.
	got := peer.seen()
	if !bytes.Equal(got.parsed, []byte{0x01, 0x02, 0x03}) {
		t.Fatalf("ParseStateSummary carried %x, want the caller's bytes 010203", got.parsed)
	}
	if got.height != wantHeight {
		t.Fatalf("GetStateSummary carried height %d, want %d", got.height, wantHeight)
	}
}

// TestAcceptNamesTheSummaryAndReportsTheMode covers the one call that crosses
// back: Accept travels as the summary's own id, and the mode it returns is the
// peer's answer, read or refused but never invented.
func TestAcceptNamesTheSummaryAndReportsTheMode(t *testing.T) {
	id := ids.ID{0xac, 0xce, 0x97}

	cases := []struct {
		name     string
		accept   zapwire.StateSummaryAcceptResponse
		wantMode chain.StateSyncMode
		wantErr  error
	}{
		{
			name:     "dynamic",
			accept:   zapwire.StateSummaryAcceptResponse{Mode: uint8(chain.StateSyncDynamic)},
			wantMode: chain.StateSyncDynamic,
		},
		{
			name:     "static",
			accept:   zapwire.StateSummaryAcceptResponse{Mode: uint8(chain.StateSyncStatic)},
			wantMode: chain.StateSyncStatic,
		},
		{
			name:     "a mode the vocabulary has no word for is refused",
			accept:   zapwire.StateSummaryAcceptResponse{Mode: 7},
			wantMode: chain.StateSyncSkipped,
			wantErr:  errUnknownSyncMode,
		},
		{
			// A summary the peer never produced. The mode field is filled in with
			// a syncing mode, so a client reading it before the error would start
			// a sync against a summary nobody ratified.
			name:     "an id the peer never produced is refused",
			accept:   zapwire.StateSummaryAcceptResponse{Mode: uint8(chain.StateSyncDynamic), Err: zapwire.ErrorNotFound},
			wantMode: chain.StateSyncSkipped,
			wantErr:  database.ErrNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			peer := &syncPeer{
				summary: zapwire.SummaryResponse{ID: id[:], Height: 12, Bytes: []byte{0x01}},
				accept:  tc.accept,
			}
			c := connect(t, zapwire.HandlerFunc(peer.handle))
			ctx := context.Background()

			s, err := c.GetLastStateSummary(ctx)
			if err != nil {
				t.Fatalf("GetLastStateSummary: %v", err)
			}

			mode, err := s.Accept(ctx)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Accept error = %v, want %v", err, tc.wantErr)
			}
			if mode != tc.wantMode {
				t.Fatalf("Accept mode = %d, want %d", mode, tc.wantMode)
			}
			if got := peer.seen().accepted; !bytes.Equal(got, id[:]) {
				t.Fatalf("Accept named %x, want the summary's own id %x", got, id[:])
			}
		})
	}
}

// TestAbsentSummaryIsAnErrorNotAnEmptyOne covers the reply that names no
// summary. Every fixture here fills in an id, a height and bytes, so a client
// that read the fields before the verdict would hand back a summary that looks
// entirely ordinary and names state no peer holds.
func TestAbsentSummaryIsAnErrorNotAnEmptyOne(t *testing.T) {
	held := ids.ID{0x77}

	cases := []struct {
		name    string
		resp    zapwire.SummaryResponse
		wantErr error
	}{
		{
			name:    "the peer holds no such summary",
			resp:    zapwire.SummaryResponse{ID: held[:], Height: 9, Bytes: []byte{0x01}, Err: zapwire.ErrorNotFound},
			wantErr: database.ErrNotFound,
		},
		{
			name:    "the peer does not sync state at all",
			resp:    zapwire.SummaryResponse{ID: held[:], Height: 9, Bytes: []byte{0x01}, Err: zapwire.ErrorStateSyncNotImplemented},
			wantErr: chain.ErrStateSyncableVMNotImplemented,
		},
		{
			// Success, and an id no summary can be named by.
			name:    "a success naming an id of the wrong width",
			resp:    zapwire.SummaryResponse{ID: []byte{0x01, 0x02, 0x03}, Height: 9, Bytes: []byte{0x01}},
			wantErr: errMalformedSummaryID,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := connect(t, zapwire.HandlerFunc((&syncPeer{summary: tc.resp}).handle))

			s, err := c.GetLastStateSummary(context.Background())
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if s != nil {
				t.Fatalf("a reply naming no summary yielded one: id %s at height %d", s.ID(), s.Height())
			}
		})
	}
}

// TestStateSyncEnabledAnswersOrSaysWhyNot covers the fifth question: a plugin
// that says no and a plugin that cannot say are different answers, and the
// second one is an error rather than a quiet no.
func TestStateSyncEnabledAnswersOrSaysWhyNot(t *testing.T) {
	cases := []struct {
		name    string
		resp    zapwire.StateSyncEnabledResponse
		want    bool
		wantErr error
	}{
		{
			name: "the plugin syncs state",
			resp: zapwire.StateSyncEnabledResponse{Enabled: true},
			want: true,
		},
		{
			name: "the plugin has it turned off",
			resp: zapwire.StateSyncEnabledResponse{Enabled: false},
			want: false,
		},
		{
			// Enabled is true so that a client reading it before the verdict would
			// report a syncing VM that cannot sync.
			name:    "the plugin does not sync state at all",
			resp:    zapwire.StateSyncEnabledResponse{Enabled: true, Err: zapwire.ErrorStateSyncNotImplemented},
			want:    false,
			wantErr: chain.ErrStateSyncableVMNotImplemented,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := connect(t, zapwire.HandlerFunc((&syncPeer{enabled: tc.resp}).handle))

			got, err := c.StateSyncEnabled(context.Background())
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("StateSyncEnabled = %v, want %v", got, tc.want)
			}
		})
	}
}
