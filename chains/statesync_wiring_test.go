// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// statesync_wiring_test.go — the serving handlers must reach a real VM.
//
// NewServer narrows its argument with a type assertion, so a server built over
// the wrong summary type is silent by construction: it compiles, every handler
// returns nil, and the chain answers no peer. That is indistinguishable from the
// stubs these replaced, which is why the assertion is asserted here rather than
// left to a build that cannot see it.

package chains

import (
	"context"
	"testing"

	"github.com/luxfi/consensus/engine/chain/statesync"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/vm/chain"
)

// servingVM holds one summary at one height, which is all the serving path reads.
type servingVM struct {
	chain.ChainVM
	summary *wiringSummary
}

func (v *servingVM) GetLastStateSummary(context.Context) (chain.StateSummary, error) {
	return v.summary, nil
}

func (v *servingVM) GetStateSummary(_ context.Context, height uint64) (chain.StateSummary, error) {
	if v.summary == nil || v.summary.height != height {
		return nil, database.ErrNotFound
	}
	return v.summary, nil
}

type wiringSummary struct {
	id     ids.ID
	height uint64
}

func (s *wiringSummary) ID() ids.ID     { return s.id }
func (s *wiringSummary) Height() uint64 { return s.height }
func (s *wiringSummary) Bytes() []byte  { return s.id[:] }
func (s *wiringSummary) Accept(context.Context) (chain.StateSyncMode, error) {
	return chain.StateSyncStatic, nil
}

// TestServingReachesTheVM is the failure this file exists for: the type the
// handlers instantiate the server with must be the type the VM's methods return,
// or the narrowing fails and the chain silently answers nothing.
func TestServingReachesTheVM(t *testing.T) {
	want := &wiringSummary{id: ids.GenerateTestID(), height: 4242}
	vm := &servingVM{summary: want}

	server := statesync.NewServer[chain.StateSummary](vm)

	got, err := server.Frontier(context.Background())
	if err != nil {
		t.Fatalf("frontier: %v — the server did not reach the VM", err)
	}
	if string(got) != string(want.Bytes()) {
		t.Fatalf("frontier returned %x, want %x", got, want.Bytes())
	}

	ids4242, err := server.Accepted(context.Background(), []uint64{4242})
	if err != nil {
		t.Fatalf("accepted: %v", err)
	}
	if len(ids4242) != 1 || ids4242[0] != want.id {
		t.Fatalf("accepted returned %v, want [%s]", ids4242, want.id)
	}
}

// TestServingIsSilentOverTheWrongType is the control: instantiate with a summary
// type the VM does not return and the server must go quiet, which is the state
// this whole file exists to detect.
func TestServingIsSilentOverTheWrongType(t *testing.T) {
	vm := &servingVM{summary: &wiringSummary{id: ids.GenerateTestID(), height: 1}}

	if _, err := statesync.NewServer[otherSummary](vm).Frontier(context.Background()); err == nil {
		t.Fatal("a server built over a summary type the VM does not return answered anyway")
	}
}

// otherSummary satisfies statesync.Summary but is not what the VM returns.
type otherSummary interface {
	ID() ids.ID
	Bytes() []byte
	metaphoricallyDifferent()
}
