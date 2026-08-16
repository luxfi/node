// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"context"
	"errors"
	"fmt"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/ids"
	"github.com/luxfi/vm/chain"
)

// State sync across the plugin boundary.
//
// A state-syncable VM answers five questions. Four of them name a summary, and
// a named summary is then accepted, which is what starts the sync. The summary
// is an object on the plugin's side: only what a caller can read off it — id,
// height, bytes — crosses. Accept therefore travels back as the id alone, and
// the plugin resolves it against the summary it handed out under that id.

// The wrapper VM asserts this interface on the VM it wraps to decide whether the
// chain can sync state at all. A Client missing any one of these methods answers
// no for every plugin-hosted chain, whatever the plugin behind it can do.
var _ chain.StateSyncableVM = (*Client)(nil)

// errMalformedSummaryID reports a reply that claims success while naming a
// summary id of the wrong width. The id is the only handle Accept has on the
// summary, so a wrong-width one cannot be repaired: padding or truncating it
// names a different summary, or none.
var errMalformedSummaryID = errors.New("zap: plugin returned malformed state summary id")

// errUnknownSyncMode reports a mode byte outside the three defined modes.
var errUnknownSyncMode = errors.New("zap: plugin returned unknown state sync mode")

// StateSyncEnabled reports whether the plugin syncs state. A plugin that cannot
// answer has not answered false — the error travels and the caller decides.
func (c *Client) StateSyncEnabled(ctx context.Context) (bool, error) {
	if !c.syncCapable.Load() {
		return false, chain.ErrStateSyncableVMNotImplemented
	}
	resp := &zapwire.StateSyncEnabledResponse{}
	if err := c.ask(ctx, zapwire.MsgStateSyncEnabled, nil, resp); err != nil {
		return false, err
	}
	if resp.Err != zapwire.ErrorUnspecified {
		return false, errorFromZAP(resp.Err)
	}
	return resp.Enabled, nil
}

// GetOngoingSyncStateSummary names the summary of a sync already under way, so
// a node that was interrupted resumes the one it started rather than beginning
// again at whatever the network now offers.
func (c *Client) GetOngoingSyncStateSummary(ctx context.Context) (chain.StateSummary, error) {
	return c.askSummary(ctx, zapwire.MsgGetOngoingSyncStateSummary, nil)
}

// GetLastStateSummary names the most recent summary the plugin holds. This is
// what a node offers when a peer asks what it can serve.
func (c *Client) GetLastStateSummary(ctx context.Context) (chain.StateSummary, error) {
	return c.askSummary(ctx, zapwire.MsgGetLastStateSummary, nil)
}

// ParseStateSummary reads summary bytes received from a peer. The plugin decides
// whether the bytes name a summary it can serve; a summary comes back only if
// they do.
func (c *Client) ParseStateSummary(ctx context.Context, summaryBytes []byte) (chain.StateSummary, error) {
	return c.askSummary(ctx, zapwire.MsgParseStateSummary, &zapwire.ParseStateSummaryRequest{Bytes: summaryBytes})
}

// GetStateSummary names the summary at a height, which is how a node checks
// whether it holds the summary its peers have settled on.
func (c *Client) GetStateSummary(ctx context.Context, height uint64) (chain.StateSummary, error) {
	return c.askSummary(ctx, zapwire.MsgGetStateSummary, &zapwire.GetStateSummaryRequest{Height: height})
}

// askSummary asks one of the four questions that answer with a summary and turns
// the answer into one. They differ only in which summary they name, never in
// what a summary is, so they share this body.
//
// An answer that names no summary is an error and never an empty summary: a
// caller handed a zero id at height zero would read it as a real one and offer
// it to the network.
func (c *Client) askSummary(ctx context.Context, op zapwire.MessageType, req encoder) (chain.StateSummary, error) {
	if !c.syncCapable.Load() {
		return nil, chain.ErrStateSyncableVMNotImplemented
	}
	resp := &zapwire.SummaryResponse{}
	if err := c.ask(ctx, op, req, resp); err != nil {
		return nil, err
	}
	if resp.Err != zapwire.ErrorUnspecified {
		return nil, errorFromZAP(resp.Err)
	}
	id, err := summaryIDFromZAP(resp.ID)
	if err != nil {
		return nil, err
	}
	return &zapSummary{
		client: c,
		id:     id,
		height: resp.Height,
		bytes:  resp.Bytes,
	}, nil
}

// zapSummary is a summary living on the plugin's side. Its readable fields were
// copied over when it was named; Accept is the one thing that has to cross back,
// and it crosses as the id.
type zapSummary struct {
	client *Client
	id     ids.ID
	height uint64
	bytes  []byte
}

func (s *zapSummary) ID() ids.ID     { return s.id }
func (s *zapSummary) Height() uint64 { return s.height }
func (s *zapSummary) Bytes() []byte  { return s.bytes }

// Accept asks the plugin to accept the summary this one names and reports which
// way the plugin decided to sync. The mode is read from the answer and never
// guessed: on any failure the returned mode is the zero mode, which a caller
// must not read before reading the error.
func (s *zapSummary) Accept(ctx context.Context) (chain.StateSyncMode, error) {
	resp := &zapwire.StateSummaryAcceptResponse{}
	req := &zapwire.StateSummaryAcceptRequest{ID: s.id[:]}
	if err := s.client.ask(ctx, zapwire.MsgStateSummaryAccept, req, resp); err != nil {
		return chain.StateSyncSkipped, err
	}
	if resp.Err != zapwire.ErrorUnspecified {
		return chain.StateSyncSkipped, errorFromZAP(resp.Err)
	}
	return syncMode(resp.Mode)
}

// syncMode reads the mode byte. A byte the vocabulary has no word for is
// refused: the mode decides whether the node keeps the state it has or throws it
// away, and there is no safe reading of a value we cannot name.
func syncMode(b uint8) (chain.StateSyncMode, error) {
	switch m := chain.StateSyncMode(b); m {
	case chain.StateSyncSkipped, chain.StateSyncStatic, chain.StateSyncDynamic:
		return m, nil
	default:
		return chain.StateSyncSkipped, fmt.Errorf("%w: %d", errUnknownSyncMode, b)
	}
}

// summaryIDFromZAP converts the id field of a summary reply into an ids.ID.
// Mirrors blockIDFromZAP: a field of any other width is refused rather than
// padded, truncated, or read as ids.Empty, which is itself a legitimate id.
func summaryIDFromZAP(b []byte) (ids.ID, error) {
	if len(b) != ids.IDLen {
		return ids.Empty, fmt.Errorf("%w: was %d bytes, want %d", errMalformedSummaryID, len(b), ids.IDLen)
	}
	return ids.ToID(b)
}

// encoder is what a request value can do. A call with no request passes nil.
type encoder interface{ Encode(*zapwire.Buffer) }

// decoder is what a reply value can do.
type decoder interface{ Decode(*zapwire.Reader) error }

// ask performs one exchange: it encodes the request, sends it under op, refuses
// a reply that answers a different question, and decodes the reply.
//
// Every reply carries MsgResponseFlag and an error reply also carries
// MsgErrorFlag — the transport has already turned that one into a non-nil error
// — so both are stripped before the reply is compared to the question asked.
func (c *Client) ask(ctx context.Context, op zapwire.MessageType, req encoder, resp decoder) error {
	var payload []byte
	if req != nil {
		buf := zapwire.GetBuffer()
		defer zapwire.PutBuffer(buf)
		req.Encode(buf)
		payload = buf.Bytes()
	}

	respType, respData, err := c.conn.Call(ctx, op, payload)
	if err != nil {
		return err
	}
	if respType&^(zapwire.MsgResponseFlag|zapwire.MsgErrorFlag) != op {
		return ErrInvalidResponse
	}
	return resp.Decode(zapwire.NewReader(respData))
}
