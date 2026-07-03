// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lp181

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/vms/proposervm/block"
)

// TestNewEpoch is the LP-181 epoch-derivation conformance suite — the luxfi
// port of avalanchego's acp181 TestNewEpoch, adapted to our NewEpoch, which is
// a PURE function of (parentEpoch, parentTimestamp, EpochDuration): unlike ava,
// it carries no fork gate (the caller decides whether epochs are active) and it
// ignores the child timestamp. It locks the three transitions that decide a
// child block's epoch — seed, hold, and seal — plus the single-increment rule.
func TestNewEpoch(t *testing.T) {
	const epochDur = 30 * time.Second
	cfg := upgrade.Config{EpochDuration: epochDur}
	base := time.Unix(1_700_000_000, 0) // fixed instant, whole second

	epoch1 := block.Epoch{PChainHeight: 100, Number: 1, StartTime: base.Unix()}

	tests := []struct {
		name               string
		parentPChainHeight uint64
		parentEpoch        block.Epoch
		parentTimestamp    time.Time
		childTimestamp     time.Time
		want               block.Epoch
	}{
		{
			// An epochless parent seeds the first epoch (Number 1), anchored at
			// the PARENT's timestamp, carrying the parent's P-Chain height.
			name:               "seed_first_epoch_from_epochless_parent",
			parentPChainHeight: 100,
			parentEpoch:        block.Epoch{},
			parentTimestamp:    base,
			childTimestamp:     base.Add(time.Second),
			want:               epoch1,
		},
		{
			// Parent issued strictly BEFORE its epoch ends did not seal it — the
			// child stays in the same epoch (unchanged, including P-Chain height).
			name:               "hold_same_epoch_parent_within_window",
			parentPChainHeight: 101,
			parentEpoch:        epoch1,
			parentTimestamp:    base.Add(epochDur / 2),
			childTimestamp:     base.Add(epochDur),
			want:               epoch1,
		},
		{
			// Parent AT the exact epoch-end boundary seals it (end is inclusive:
			// the guard is `Before`, not `!After`) — child opens epoch Number+1.
			name:               "seal_barely_transition_at_epoch_end",
			parentPChainHeight: 101,
			parentEpoch:        epoch1,
			parentTimestamp:    base.Add(epochDur),
			childTimestamp:     base.Add(epochDur),
			want:               block.Epoch{PChainHeight: 101, Number: 2, StartTime: base.Add(epochDur).Unix()},
		},
		{
			// Parent MANY epochs stale still advances by exactly ONE (Number+1),
			// re-anchored at the parent's timestamp. This single-increment rule is
			// the mechanism behind the fleet's observed "epoch increments per
			// block" once every parent was hours stale — the churn the proposervm
			// slot-snap liveness fix removes by keeping successive parent
			// timestamps on a stable grid instead of a jumping wall clock.
			name:               "seal_transition_parent_well_past_epoch",
			parentPChainHeight: 101,
			parentEpoch:        epoch1,
			parentTimestamp:    base.Add(3 * epochDur),
			childTimestamp:     base.Add(4 * epochDur),
			want:               block.Epoch{PChainHeight: 101, Number: 2, StartTime: base.Add(3 * epochDur).Unix()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewEpoch(cfg, tt.parentPChainHeight, tt.parentEpoch, tt.parentTimestamp, tt.childTimestamp)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestNewEpoch_InvariantToChildTimestamp proves the epoch a child is assigned
// does NOT depend on the child's own timestamp — only on (parentEpoch,
// parentTimestamp, EpochDuration). This is the property that makes the
// proposervm slot-snap liveness fix epoch-SAFE: snapping the child's timestamp
// to the proposer-window grid (block.go buildChild) cannot move the block's
// epoch, so ExpectedProposer's epoch-derived validator-set resolution is
// unchanged. It also pins WHERE epoch churn comes from — the PARENT timestamp —
// so that stabilising successive blocks' timestamps stabilises the whole epoch
// sequence.
func TestNewEpoch_InvariantToChildTimestamp(t *testing.T) {
	const epochDur = 30 * time.Second
	cfg := upgrade.Config{EpochDuration: epochDur}
	base := time.Unix(1_700_000_000, 0)
	parentEpoch := block.Epoch{PChainHeight: 100, Number: 7, StartTime: base.Unix()}
	parentTimestamp := base.Add(epochDur / 3) // within window: child must hold epoch 7

	want := NewEpoch(cfg, 101, parentEpoch, parentTimestamp, parentTimestamp)
	for _, childOffset := range []time.Duration{
		-time.Hour, 0, time.Millisecond, time.Second, epochDur, 7 * epochDur, 48 * time.Hour,
	} {
		child := parentTimestamp.Add(childOffset)
		got := NewEpoch(cfg, 101, parentEpoch, parentTimestamp, child)
		require.Equalf(t, want, got,
			"epoch must be invariant to child timestamp (offset %v): slot-snap would be unsafe otherwise", childOffset)
	}
}
