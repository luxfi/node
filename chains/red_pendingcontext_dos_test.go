// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// red_pendingcontext_dos_test.go — regression guard for the catch-up DoS RED found
// on the cert-carrying catch-up branch.
//
// The frontier-sync wiring connects the AcceptedFrontier handler to
// requestContext(), which records each requested blockID in b.pendingContext.
// Originally NOTHING evicted from that map, so a Byzantine peer streaming
// AcceptedFrontier frames each naming a distinct random tip grew it without bound
// → OOM (and a peer that took a request then withheld Context re-stranded the
// victim forever). requestContext now reaps entries past pendingContextTTL and
// hard-caps the map at maxPendingContext. These tests pin both properties; before
// the fix the first asserted N=50_000 entries with ZERO eviction.
package chains

import (
	"context"
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network"
	"github.com/luxfi/node/proto/p2p"
)

// redStubNet implements the one Network method requestContext uses (Send); every
// other method is inherited from the embedded nil interface and is never called.
type redStubNet struct {
	network.Network
	sends int
}

func (s *redStubNet) Send(_ message.OutboundMessage, nodeIDs set.Set[ids.NodeID], _ ids.ID, _ uint32) set.Set[ids.NodeID] {
	s.sends++
	return nodeIDs
}

// redStubMsg implements the one OutboundMsgBuilder method requestContext uses.
type redStubMsg struct {
	message.OutboundMsgBuilder
}

func (redStubMsg) GetAncestors(_ ids.ID, _ uint32, _ time.Duration, _ ids.ID, _ p2p.EngineType) (message.OutboundMessage, error) {
	return nil, nil
}

func newRedTestHandler(net network.Network) *blockHandler {
	return &blockHandler{
		logger:         log.NewNoOpLogger(),
		net:            net,
		msgCreator:     redStubMsg{},
		chainID:        ids.GenerateTestID(),
		pendingContext: make(map[ids.ID]contextRequest),
	}
}

// TestPendingContext_BoundedUnderFlood: a peer streaming distinct fake tips into
// requestContext can NEVER grow pendingContext past maxPendingContext. Inverts the
// original RED PoC, which asserted unbounded growth to 50_000 → OOM.
func TestPendingContext_BoundedUnderFlood(t *testing.T) {
	stubNet := &redStubNet{}
	bh := newRedTestHandler(stubNet)

	const N = 10_000 // ≫ the maxPendingContext cap — enough to prove "stays bounded"
	from := ids.GenerateTestNodeID()
	for i := 0; i < N; i++ {
		bh.requestContext(context.Background(), from, ids.GenerateTestID())
	}

	if got := len(bh.pendingContext); got > maxPendingContext {
		t.Fatalf("pendingContext unbounded: %d entries exceeds cap %d (the RED HIGH DoS)", got, maxPendingContext)
	}
	t.Logf("bounded: %d entries after a %d-distinct-tip flood (cap %d, sends=%d)",
		len(bh.pendingContext), N, maxPendingContext, stubNet.sends)
}

// TestPendingContext_StaleEntriesReaped: a request whose Context is withheld past
// its TTL is reaped on the next requestContext, so the block is re-requestable from
// an honest peer (fixes the RED MEDIUM re-strand).
func TestPendingContext_StaleEntriesReaped(t *testing.T) {
	bh := newRedTestHandler(&redStubNet{})
	from := ids.GenerateTestNodeID()

	stale := ids.GenerateTestID()
	bh.pendingContext[stale] = contextRequest{
		nodeID:    from,
		requestID: 1,
		blockID:   stale,
		timestamp: time.Now().Add(-2 * pendingContextTTL),
	}

	// Any later request runs the reaper before recording its own entry.
	bh.requestContext(context.Background(), from, ids.GenerateTestID())

	if _, stillThere := bh.pendingContext[stale]; stillThere {
		t.Fatalf("stale pendingContext entry (%v old) not reaped → re-strand persists", 2*pendingContextTTL)
	}
}

// TestPendingContext_StaleEntryForTheSameBlockIsReRequested: the case the test
// above CANNOT see. It reaps by asking for a *different* block, which reaches the
// sweep. A node that is behind asks for exactly one block -- the one it is missing
// -- so it never takes that path: the dedup early-return fires first, every time,
// and the sweep is unreachable for the only blockID that matters.
//
// Measured on testnet luxd-3: one slot held 10.9 HOURS past a 30s TTL, 38k
// suppressions deep, while the node sat 1738 blocks behind a live chain. The
// suppression was self-sustaining -- the wedge kept the node asking for the same
// block, and asking for the same block is what kept it wedged.
//
// Fails against the pre-fix code: sends stays 1, because the expired slot
// suppresses forever.
func TestPendingContext_StaleEntryForTheSameBlockIsReRequested(t *testing.T) {
	stubNet := &redStubNet{}
	bh := newRedTestHandler(stubNet)
	from := ids.GenerateTestNodeID()
	missing := ids.GenerateTestID() // the ONE block this node needs

	bh.requestContext(context.Background(), from, missing)
	if stubNet.sends != 1 {
		t.Fatalf("setup: want 1 send, got %d", stubNet.sends)
	}

	// Positive control: while genuinely in flight, a re-ask MUST still be
	// suppressed. Expiry must not become "no dedup at all".
	bh.requestContext(context.Background(), from, missing)
	if stubNet.sends != 1 {
		t.Fatalf("live in-flight request was not deduped: sends=%d, want 1", stubNet.sends)
	}

	// The peer took the request and never answered. Age the slot past its TTL.
	req := bh.pendingContext[missing]
	req.timestamp = time.Now().Add(-2 * pendingContextTTL)
	bh.pendingContext[missing] = req

	// Same block, asked again -- the only thing a behind node ever does.
	bh.requestContext(context.Background(), from, missing)
	if stubNet.sends != 2 {
		t.Fatalf("expired request still suppressed its own block: sends=%d, want 2 "+
			"-- the block a behind node needs is the block it can never ask for again",
			stubNet.sends)
	}
}
