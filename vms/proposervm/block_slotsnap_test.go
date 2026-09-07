// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"testing"
	"time"

	"github.com/luxfi/node/vms/proposervm/proposer"
)

// TestSlotSnappedChildTimestamp proves the view-change liveness fix's properties on the
// timestamp buildChild stamps: the same-slot IDEMPOTENCE that makes a stale-parent rebuild
// one stable candidate, and the SLOT-INVARIANCE that keeps every proposer-window check
// byte-identical to the pre-fix behaviour.
func TestSlotSnappedChildTimestamp(t *testing.T) {
	parent := time.Unix(1_700_000_000, 0) // fixed parent instant

	t.Run("idempotent_within_a_slot_off_a_stale_parent", func(t *testing.T) {
		// A parent 2h stale: slot = 1440. Two rebuilds 1.9s apart land in the SAME slot and
		// MUST produce the byte-identical timestamp (→ identical envelope id). This is the
		// property that collapses the unbounded churn to one candidate.
		base := parent.Add(2 * time.Hour)
		a := slotSnappedChildTimestamp(parent, base.Add(200*time.Millisecond))
		b := slotSnappedChildTimestamp(parent, base.Add(2100*time.Millisecond))
		if !a.Equal(b) {
			t.Fatalf("NOT idempotent within a slot: %v != %v (churn not eliminated)", a, b)
		}
		// And it is exactly on the window grid.
		if off := a.Sub(parent) % proposer.WindowDuration; off != 0 {
			t.Fatalf("snapped timestamp %v is not on the WindowDuration grid (offset %v)", a, off)
		}
	})

	t.Run("slot_invariant_proposer_window_unchanged", func(t *testing.T) {
		// For a spread of stale ages, the snapped timestamp MUST map to the SAME slot as the
		// raw now — this is what guarantees ExpectedProposer returns the identical verdict, so
		// the fix cannot cause errUnexpectedProposer / errProposerWindowNotStarted.
		for _, age := range []time.Duration{
			6 * time.Second, 5*time.Minute + time.Second, time.Hour, 3 * time.Hour,
		} {
			now := parent.Add(age)
			snapped := slotSnappedChildTimestamp(parent, now)
			if got, want := proposer.TimeToSlot(parent, snapped), proposer.TimeToSlot(parent, now.Truncate(time.Second)); got != want {
				t.Fatalf("age %v: snapped slot %d != raw slot %d — proposer-window verdict would change", age, got, want)
			}
		}
	})

	t.Run("monotonic_and_not_advanced", func(t *testing.T) {
		for _, age := range []time.Duration{
			5 * time.Second, 7 * time.Second, time.Hour,
		} {
			now := parent.Add(age)
			snapped := slotSnappedChildTimestamp(parent, now)
			if !snapped.After(parent) {
				t.Fatalf("age %v: snapped %v not strictly after parent %v (errTimeNotMonotonic risk)", age, snapped, parent)
			}
			if snapped.After(now) {
				t.Fatalf("age %v: snapped %v runs ahead of now %v (errTimeTooAdvanced risk)", age, snapped, now)
			}
		}
	})

	t.Run("fresh_in_window_child_unchanged", func(t *testing.T) {
		// Slot 0 (child < WindowDuration after parent) is the NORMAL live case: no snap, exact
		// wall-clock (truncated) timestamp — zero behaviour change on a healthy chain.
		now := parent.Add(3 * time.Second)
		if got, want := slotSnappedChildTimestamp(parent, now), now.Truncate(time.Second); !got.Equal(want) {
			t.Fatalf("in-window child was altered: got %v want %v", got, want)
		}
	})

	t.Run("clock_before_parent_pins_to_parent", func(t *testing.T) {
		now := parent.Add(-10 * time.Second)
		if got := slotSnappedChildTimestamp(parent, now); !got.Equal(parent) {
			t.Fatalf("now<parent must pin to parent, got %v", got)
		}
	})
}
