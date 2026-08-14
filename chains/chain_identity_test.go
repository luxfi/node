// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/network"
	"github.com/luxfi/node/network/peer"
)

// A node runs eight chains through one writer. The catch-up lines carry heights and
// block ids only, and chains created together sit in the same height range, so a
// height cannot name a chain. buildChain therefore tags ONE logger per chain and
// hands it to everything the chain is built from; these tests pin that contract on
// the per-chain inbound handler, which emits the catch-up lines.

func capture() (log.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return log.NewWriter(buf), buf
}

// TestHandlerLinesNameTheirChain: a line the handler emits identifies the chain it
// speaks for, and says so ONCE. requestContext with no transport is the shortest
// reachable emitter.
//
// The handler is built the way buildChain builds it — given the chain's logger,
// carrying nothing of its own. A second stamp inside the constructor would put two
// "chain" fields on every line, which is why the count is asserted, not just
// presence.
func TestHandlerLinesNameTheirChain(t *testing.T) {
	logger, buf := capture()
	chainID := ids.GenerateTestID()

	bh := newBlockHandler(nil, nil, speaksFor(logger, chainID), nil, nil, nil, chainID, ids.Empty, nil, ids.NodeID{}, false, true)
	bh.requestContext(context.Background(), ids.NodeID{}, ids.GenerateTestID())

	out := buf.String()
	if out == "" {
		t.Fatal("no line was emitted — the probe is broken, not the code")
	}
	if n := strings.Count(out, chainID.String()); n != 1 {
		t.Fatalf("handler line names its chain %s %d times, want exactly 1.\n"+
			"Zero: eight chains share one writer and the catch-up lines carry only heights,\n"+
			"so the line is unattributable. Twice: a second site is stamping an identity the\n"+
			"chain's logger already carries.\nline: %s", chainID, n, out)
	}
}

// noPeers reports an empty peer set, which is the shortest way into the frontier
// poll's own log line.
type noPeers struct{ network.Network }

func (noPeers) PeerInfo([]ids.NodeID) []peer.Info { return nil }

// TestFrontierLinesNameTheirChain: the frontier lines used to carry an explicit
// chainID field of their own. They no longer do — the chain's logger carries it —
// so this pins that they are still attributable.
func TestFrontierLinesNameTheirChain(t *testing.T) {
	logger, buf := capture()
	chainID := ids.GenerateTestID()

	bh := newBlockHandler(nil, nil, speaksFor(logger, chainID), nil, noPeers{}, nil, chainID, ids.Empty, nil, ids.NodeID{}, false, true)
	bh.pollFrontierOnce(context.Background())

	out := buf.String()
	if !strings.Contains(out, "frontier poll") {
		t.Fatalf("no frontier line was emitted — the probe is broken, not the code: %q", out)
	}
	if n := strings.Count(out, chainID.String()); n != 1 {
		t.Fatalf("frontier line names its chain %s %d times, want exactly 1: %s", chainID, n, out)
	}
}

// Negative control for the test above: the tag must be THIS chain, not a constant.
// A fix that stamped a fixed string, or that stamped the network id, passes the
// positive test and fails this one.
func TestHandlerLinesNameTheOwnChainNotAnother(t *testing.T) {
	mine := ids.GenerateTestID()
	other := ids.GenerateTestID()

	logger, buf := capture()
	bh := newBlockHandler(nil, nil, speaksFor(logger, mine), nil, nil, nil, mine, ids.Empty, nil, ids.NodeID{}, false, true)
	bh.requestContext(context.Background(), ids.NodeID{}, ids.GenerateTestID())

	out := buf.String()
	if strings.Contains(out, other.String()) {
		t.Fatalf("handler on chain %s emitted a line naming a different chain %s: %s", mine, other, out)
	}
	if !strings.Contains(out, mine.String()) {
		t.Fatalf("handler on chain %s emitted a line naming no chain: %s", mine, out)
	}
}

// TestSpeaksForNamesTheChain covers the helper buildChain builds the chain's logger
// with — the logger the consensus runtime, the gossiper, the catch-up wire and the
// VM all write through, and the one that emits the finality guard's cert refusal.
//
// HONEST LIMIT: this exercises the helper, NOT the buildChain call site. buildChain
// needs a fully constructed manager (VM registry, network, router), so reverting its
// chainLog to the untagged m.Log would leave this test green. The handler tests above
// bite on that revert only through the second half of it — restoring the constructor's
// own stamp — which is why they count the tag rather than look for it.
func TestSpeaksForNamesTheChain(t *testing.T) {
	logger, buf := capture()
	chainID := ids.GenerateTestID()

	speaksFor(logger, chainID).Warn("incoming cert: REFUSED by finality guard (no VM.Accept)")

	out := buf.String()
	if !strings.Contains(out, chainID.String()) {
		t.Fatalf("tagged line does not name its chain %s: %s", chainID, out)
	}
}

// speaksFor must not turn a disabled logger into a live one, and must not panic on
// one. Chains are built with log.Noop() in several paths.
func TestSpeaksForLeavesADisabledLoggerAlone(t *testing.T) {
	if got := speaksFor(log.Noop(), ids.GenerateTestID()); got == nil || !got.IsZero() {
		t.Fatalf("a disabled logger must stay disabled; got %#v", got)
	}
	if got := speaksFor(nil, ids.GenerateTestID()); got != nil {
		t.Fatalf("a nil logger must stay nil; got %#v", got)
	}
}
