// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	vmchain "github.com/luxfi/vm/chain"

	"github.com/luxfi/node/vms/proposervm/state"
)

// stubInnerVM embeds the ChainVM interface (nil) and overrides ONLY GetBlock to report
// "not found", so getBlock (getPostForkBlock miss → getPreForkBlock → ChainVM.GetBlock)
// cleanly errors for an unheld id without a panic. No other inner method is invoked on
// the miss path under test — the #1 fix returns before any inner delegation.
type stubInnerVM struct{ vmchain.ChainVM }

func (stubInnerVM) GetBlock(context.Context, ids.ID) (vmchain.Block, error) {
	return nil, errors.New("stub inner VM: block not found")
}

// newWedgeTestVM builds the minimal proposervm needed to exercise SetPreference's
// getBlock path: an empty on-disk State (post-fork miss) + a stub inner VM (pre-fork
// miss) ⇒ getBlock cleanly errors for any id not pre-seeded, exactly as a validator
// that fell behind the frontier sees a build-tip above its own store.
func newWedgeTestVM(priorPreferred ids.ID) *VM {
	vm := &VM{
		preferred:      priorPreferred,
		verifiedBlocks: map[ids.ID]PostForkBlock{},
		logger:         log.Noop(),
	}
	vm.State = state.New(versiondb.New(memdb.New()))
	vm.ChainVM = stubInnerVM{}
	return vm
}

// TestSetPreference_UnheldBlock_DoesNotPoison is the deterministic unit repro of the
// luxd-1 REJOIN WEDGE (defect #1).
//
// A validator that fell behind is steered by the consensus engine
// (consensus/engine/chain/integration.go — defect #2) to a build tip ABOVE its own
// frontier: an id this proposervm does not hold. The OLD SetPreference assigned
// vm.preferred = <unheld id> BEFORE validating, so on the (inevitable) getBlock miss it
// returned a hard error but LEFT vm.preferred poisoned. BuildBlock then read the poisoned
// id (getBlock) and logged "unexpected build block failure / failed to fetch preferred
// block" on EVERY build attempt forever — Quasar cert-finality has no re-converge poll, so
// the node never recovered.
//
// The fix validates BEFORE assigning: on an unheld id, vm.preferred is UNCHANGED (kept
// prior-good) and SetPreference is a non-fatal no-op, so BuildBlock stays live on the last
// held tip while the catch-up path pulls the gap.
func TestSetPreference_UnheldBlock_DoesNotPoison(t *testing.T) {
	require := require.New(t)

	priorGood := ids.GenerateTestID()
	vm := newWedgeTestVM(priorGood)

	unheld := ids.GenerateTestID()
	err := vm.SetPreference(context.Background(), unheld)

	// An unheld build hint must be a non-fatal no-op — NEVER a hard error that also leaves
	// the id poisoned (the old behavior that wedged BuildBlock).
	require.NoError(err, "unheld SetPreference must not be fatal")

	// THE anti-wedge invariant: vm.preferred is NOT poisoned to the unheld id.
	require.Equal(priorGood, vm.preferred,
		"vm.preferred must stay the prior-good tip; poisoning it wedges BuildBlock forever")
	require.NotEqual(unheld, vm.preferred)
}

// TestSetPreference_UnheldBlock_KeepsBuildLive asserts the DOWNSTREAM property the fix
// exists to protect: after an unheld SetPreference, the preference the VM will build on is
// still the prior-good (held) tip, not the poisoned id — i.e. BuildBlock would fetch a
// block it holds, not spam "failed to fetch preferred block". We assert on vm.preferred
// (what BuildBlock reads) rather than driving a full inner-VM build, keeping the repro
// hermetic.
func TestSetPreference_UnheldBlock_KeepsBuildLive(t *testing.T) {
	require := require.New(t)

	priorGood := ids.GenerateTestID()
	vm := newWedgeTestVM(priorGood)

	// Repeated unheld steers (the engine re-steers on every gossip receipt) must never
	// accumulate poison: the build preference stays pinned to the held tip.
	for i := 0; i < 5; i++ {
		require.NoError(vm.SetPreference(context.Background(), ids.GenerateTestID()))
		require.Equal(priorGood, vm.preferred, "build tip must remain the held prior-good after steer %d", i)
	}
}
