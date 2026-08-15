// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"time"

	"github.com/luxfi/metric"
)

// A chain that stops moving must be visible without reading a log.
//
// Everything below already existed inside the process and was reachable only by
// opening a pod's output and knowing which of eight chains was speaking: how far
// finality has got, how far the VM has got, whether initial sync is still running,
// and which arm each catch-up attempt took. Diagnosing a stalled chain therefore
// meant grepping, and a grep that matches nothing looks exactly like a chain with
// nothing wrong.
//
// The counters are NOT re-counted here. blockHandler already maintains them as
// atomics to pace its own logging; this samples them. One source of truth, so a
// metric and a log line can never disagree about the same event.

// sampleEvery is the projection interval. Scrapes are coarser than this, so a
// sample per second costs one atomic load per counter and never misses a step
// that a scrape would have seen.
const sampleEvery = time.Second

// observe projects this chain's state onto metrics until ctx ends.
//
// Heights are read through the engine, which holds them under its own lock; the
// VM's applied head is read with a bound because it reaches into the VM, and a
// metric must never be able to wedge on a chain that is already unwell.
func (b *blockHandler) observe(ctx context.Context, m metric.Metrics) {
	if m == nil || b == nil {
		return
	}

	finalized := m.NewGauge("finalized_height",
		"height this chain has finalized in consensus")
	applied := m.NewGauge("applied_height",
		"height the VM has executed; below finalized_height means blocks are decided but not run")
	syncing := m.NewGauge("syncing",
		"1 while initial sync is driving this chain, 0 once it is live")
	unparsed := m.NewGauge("blocks_unparsed",
		"blocks received that did not parse, cumulative since start; a rise means blocks are being dropped")

	// Cumulative since start, so a rise means the arm was taken. The names are the
	// question each answers: did we ask, were we served, did a reply arrive, and
	// what did the frontier decide.
	outcome := m.NewGaugeVec("catchup_outcomes",
		"cumulative catch-up attempts by outcome since start", []string{"outcome"})

	arms := []struct {
		name string
		read func() uint64
	}{
		{"no_wire", b.diag.noWire.Load},
		{"deduped", b.diag.dedup.Load},
		{"expired", b.diag.reaped.Load},
		{"evicted", b.diag.evicted.Load},
		{"no_peer", b.diag.noPeer.Load},
		{"unsent", b.diag.unsent.Load},

		{"serve_no_wire", b.diag.serveNoWire.Load},
		{"serve_asked", b.diag.serveAsked.Load},
		{"serve_empty", b.diag.serveEmpty.Load},
		{"serve_busy", b.diag.serveBusy.Load},

		{"reply_in", b.diag.replyIn.Load},
		{"reply_no_vm", b.diag.replyNoVM.Load},
		{"reply_empty", b.diag.replyEmpty.Load},
		{"reply_to_sync", b.diag.replyBootstrap.Load},

		{"frontier_to_sync", b.diag.frontierBootstrap.Load},
		{"frontier_current", b.diag.frontierCurrent.Load},
		{"frontier_cert_gap", b.diag.frontierCertGap.Load},
		{"frontier_pending", b.diag.frontierPending.Load},
		{"frontier_absent", b.diag.frontierAbsent.Load},
		{"frontier_no_engine", b.diag.frontierNoEngine.Load},
	}

	// Sample BEFORE waiting. A gauge exists from the moment it is created, so
	// ticking first leaves every value at zero for one interval — and a scrape
	// landing in that window reads a chain with no traffic and nothing synced,
	// which is a specific and wrong claim rather than an absent one.
	tick := time.NewTicker(sampleEvery)
	defer tick.Stop()
	for {
		for _, a := range arms {
			outcome.WithLabelValues(a.name).Set(float64(a.read()))
		}

		unparsed.Set(float64(b.diag.blockUnparsed.Load()))

		if b.bsActive.Load() {
			syncing.Set(1)
		} else {
			syncing.Set(0)
		}

		if b.engine == nil {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			continue
		}
		if _, h, ok := b.engine.FinalizedLedger(); ok {
			finalized.Set(float64(h))
		}

		// Bounded: this reads the VM, and an unwell chain is exactly when it is
		// slowest. A sample we skip costs nothing; a sample that blocks costs the
		// scrape.
		read, cancel := context.WithTimeout(ctx, sampleEvery)
		_, h, err := b.engine.AppliedHead(read)
		cancel()
		if err == nil {
			applied.Set(float64(h))
		}

		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
