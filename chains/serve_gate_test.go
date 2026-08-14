// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// serve_gate_test.go — a peer must not be able to choose how much of our memory it
// spends.
//
// Answering a catch-up request walks up to maxContextBlocks blocks out of the VM and
// assembles them into one multi-hundred-kilobyte message. The request RATE is not
// ours to pick: a node that has fallen behind asks every peer, as fast as its timer
// allows, for as long as it stays behind. Unbounded, the number of answers under
// construction becomes this node's memory ceiling.

package chains

import (
	"sync"
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
)

func gatedHandler() *blockHandler {
	return &blockHandler{
		servingPeers: set.NewSet[ids.NodeID](16),
		servingSlots: make(chan struct{}, maxConcurrentServes),
	}
}

// TestOneAnswerPerPeer: a peer we are already answering does not get a second
// assembly started on its behalf. This is the bound that matters, because the flood
// comes from ONE behind node re-asking, not from many peers asking once.
func TestOneAnswerPerPeer(t *testing.T) {
	b := gatedHandler()
	peer := ids.GenerateTestNodeID()

	if !b.beginServe(peer) {
		t.Fatal("first request from a peer must be answered")
	}
	if b.beginServe(peer) {
		t.Fatal("a peer already being answered must not start a second assembly")
	}

	b.endServe(peer)
	if !b.beginServe(peer) {
		t.Fatal("after finishing, the same peer must be servable again")
	}
	b.endServe(peer)
}

// TestServingIsCappedAcrossPeers: the per-peer rule alone still lets N peers cost N
// assemblies, so there is a ceiling over all of them.
func TestServingIsCappedAcrossPeers(t *testing.T) {
	b := gatedHandler()
	var held []ids.NodeID

	for i := 0; i < maxConcurrentServes; i++ {
		p := ids.GenerateTestNodeID()
		if !b.beginServe(p) {
			t.Fatalf("peer %d is within the cap and must be answered", i)
		}
		held = append(held, p)
	}
	if b.beginServe(ids.GenerateTestNodeID()) {
		t.Fatalf("the %dth concurrent answer must be declined", maxConcurrentServes+1)
	}

	// Finishing one frees exactly one slot — not more, not none.
	b.endServe(held[0])
	next := ids.GenerateTestNodeID()
	if !b.beginServe(next) {
		t.Fatal("a freed slot must be reusable")
	}
	if b.beginServe(ids.GenerateTestNodeID()) {
		t.Fatal("only one slot was freed, so only one more answer may start")
	}

	b.endServe(next)
	for _, p := range held[1:] {
		b.endServe(p)
	}
}

// TestSlotsSurviveChurn: begin/end must balance exactly, or the gate slowly closes on
// a healthy node and catch-up stops working for everyone — a worse failure than the
// one the gate prevents.
func TestSlotsSurviveChurn(t *testing.T) {
	b := gatedHandler()
	for i := 0; i < maxConcurrentServes*50; i++ {
		p := ids.GenerateTestNodeID()
		if !b.beginServe(p) {
			t.Fatalf("iteration %d: gate closed on a handler with nothing in flight — a slot leaked", i)
		}
		b.endServe(p)
	}
	if len(b.servingSlots) != 0 {
		t.Fatalf("%d slots still held with nothing in flight", len(b.servingSlots))
	}
	if b.servingPeers.Len() != 0 {
		t.Fatalf("%d peers still marked in flight", b.servingPeers.Len())
	}
}

// TestConcurrentAskersCannotExceedTheCap drives the gate the way the network does —
// many goroutines at once — and asserts the cap holds and nothing leaks.
func TestConcurrentAskersCannotExceedTheCap(t *testing.T) {
	b := gatedHandler()
	var wg sync.WaitGroup
	var mu sync.Mutex
	live, peak := 0, 0

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := ids.GenerateTestNodeID()
			if !b.beginServe(p) {
				return
			}
			mu.Lock()
			live++
			if live > peak {
				peak = live
			}
			mu.Unlock()

			mu.Lock()
			live--
			mu.Unlock()
			b.endServe(p)
		}()
	}
	wg.Wait()

	if peak > maxConcurrentServes {
		t.Fatalf("%d answers were under construction at once, cap is %d", peak, maxConcurrentServes)
	}
	if len(b.servingSlots) != 0 || b.servingPeers.Len() != 0 {
		t.Fatalf("leaked after churn: %d slots, %d peers", len(b.servingSlots), b.servingPeers.Len())
	}
}
