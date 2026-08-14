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
)

// A node runs eight chains through one writer. The catch-up lines carry heights and
// block ids only, and chains created together sit in the same height range, so a
// height cannot name a chain. These tests pin the identity onto the two loggers that
// emit those lines: the per-chain inbound handler, and the consensus runtime.

func capture() (log.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return log.NewWriter(buf), buf
}

// TestHandlerLinesNameTheirChain: a line the handler emits identifies the chain it
// speaks for. requestContext with no transport is the shortest reachable emitter.
func TestHandlerLinesNameTheirChain(t *testing.T) {
	logger, buf := capture()
	chainID := ids.GenerateTestID()

	bh := newBlockHandler(nil, nil, logger, nil, nil, nil, chainID, ids.Empty, nil, ids.NodeID{}, false, true)
	bh.requestContext(context.Background(), ids.NodeID{}, ids.GenerateTestID())

	out := buf.String()
	if out == "" {
		t.Fatal("no line was emitted — the probe is broken, not the code")
	}
	if !strings.Contains(out, chainID.String()) {
		t.Fatalf("handler line does not name its chain %s.\n"+
			"Eight chains share one writer and the catch-up lines carry only heights, so an\n"+
			"unnamed line is unattributable.\nline: %s", chainID, out)
	}
}

// Negative control for the test above: the tag must be THIS chain, not a constant.
// A fix that stamped a fixed string, or that stamped the network id, passes the
// positive test and fails this one.
func TestHandlerLinesNameTheOwnChainNotAnother(t *testing.T) {
	mine := ids.GenerateTestID()
	other := ids.GenerateTestID()

	logger, buf := capture()
	bh := newBlockHandler(nil, nil, logger, nil, nil, nil, mine, ids.Empty, nil, ids.NodeID{}, false, true)
	bh.requestContext(context.Background(), ids.NodeID{}, ids.GenerateTestID())

	out := buf.String()
	if strings.Contains(out, other.String()) {
		t.Fatalf("handler on chain %s emitted a line naming a different chain %s: %s", mine, other, out)
	}
	if !strings.Contains(out, mine.String()) {
		t.Fatalf("handler on chain %s emitted a line naming no chain: %s", mine, out)
	}
}

// TestSpeaksForNamesTheChain covers the helper the consensus runtime's logger is
// built with (NetworkConfig.Logger, buildChain) — the logger that emits the finality
// guard's cert refusal, the error this whole investigation turns on.
//
// HONEST LIMIT: this exercises the helper, NOT the buildChain call site. buildChain
// needs a fully constructed manager (VM registry, network, router), so reverting
// NetworkConfig.Logger to the untagged m.Log would leave this test green. The two
// handler tests above DO cover their call site.
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
