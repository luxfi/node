// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// observe_test.go — a stalled chain has to be visible without reading a log.
//
// The counters these tests read already gated the handler's own logging. What was
// missing was any way to see them from outside the process, so a chain that stopped
// was indistinguishable from a chain with nothing to say until someone opened a pod.

package chains

import (
	"context"
	"testing"
	"time"

	"github.com/luxfi/metric"
)

// sample runs the projection until the named metric appears, or gives up. It polls
// rather than sleeping a fixed span so the test does not encode the tick interval.
func sample(t *testing.T, b *blockHandler, name string) map[string]float64 {
	t.Helper()

	reg := metric.NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.observe(ctx, reg)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		families, err := reg.Gather()
		if err != nil {
			t.Fatalf("gather: %v", err)
		}
		for _, f := range families {
			if f.Name != name {
				continue
			}
			out := map[string]float64{}
			for _, m := range f.Metrics {
				key := ""
				for _, l := range m.Labels {
					key = l.Value
				}
				out[key] = m.Value.Value
			}
			if len(out) > 0 {
				return out
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("metric %q never appeared", name)
	return nil
}

// TestObserveReportsWhatTheCountersSay is the whole point: an operator can tell
// which arm the catch-up path took without a log.
func TestObserveReportsWhatTheCountersSay(t *testing.T) {
	b := &blockHandler{}
	b.diag.replyIn.Store(7)
	b.diag.noPeer.Store(3)
	b.diag.frontierCertGap.Store(11)

	got := sample(t, b, "catchup_outcomes")

	for name, want := range map[string]float64{
		"reply_in":          7,
		"no_peer":           3,
		"frontier_cert_gap": 11,
	} {
		if got[name] != want {
			t.Errorf("outcome %q = %v, want %v — the projection disagrees with the counter it reads",
				name, got[name], want)
		}
	}
}

// TestObserveTracksACounterThatMoves: a stalled chain is diagnosed from a counter
// that stops rising, so a single reading is not enough — the projection has to
// follow the atomic rather than latch its first value.
func TestObserveTracksACounterThatMoves(t *testing.T) {
	b := &blockHandler{}
	b.diag.replyIn.Store(1)

	reg := metric.NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.observe(ctx, reg)

	read := func() float64 {
		families, _ := reg.Gather()
		for _, f := range families {
			if f.Name != "catchup_outcomes" {
				continue
			}
			for _, m := range f.Metrics {
				for _, l := range m.Labels {
					if l.Value == "reply_in" {
						return m.Value.Value
					}
				}
			}
		}
		return -1
	}

	waitFor := func(want float64) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if read() == want {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("reply_in never reached %v (last %v) — the projection latched instead of following", want, read())
	}

	waitFor(1)
	b.diag.replyIn.Store(42)
	waitFor(42)
}

// TestObserveReportsInitialSync: "is this chain still syncing" was only answerable
// by an RPC that a wedged node can fail to serve. It is a gauge now.
func TestObserveReportsInitialSync(t *testing.T) {
	b := &blockHandler{}
	b.bsActive.Store(true)

	reg := metric.NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.observe(ctx, reg)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		families, _ := reg.Gather()
		for _, f := range families {
			if f.Name == "syncing" && len(f.Metrics) > 0 {
				if v := f.Metrics[0].Value.Value; v != 1 {
					t.Fatalf("syncing = %v while initial sync is driving, want 1", v)
				}
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("syncing gauge never appeared")
}

// TestObserveSurvivesAHandlerWithNoEngine: the projection runs on every chain,
// including ones built without a consensus engine. It must degrade to reporting
// what it can rather than panicking and taking the chain with it.
func TestObserveSurvivesAHandlerWithNoEngine(t *testing.T) {
	b := &blockHandler{} // engine is nil
	b.diag.serveAsked.Store(5)

	got := sample(t, b, "catchup_outcomes")
	if got["serve_asked"] != 5 {
		t.Fatalf("serve_asked = %v, want 5", got["serve_asked"])
	}
}
