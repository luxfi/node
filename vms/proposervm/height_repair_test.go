// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import "testing"

// TestPlanHeightRepair locks the init-time reconciliation policy between the
// proposervm finality index and the inner VM's accepted tip. The load-bearing
// case is repairResetBehindIndex: proposervm BEHIND the inner (e.g. the devnet-C
// "index 7 < inner 8" truncated snapshot) must map to a recoverable reset, NOT
// the pre-fix behavior that returned a fatal init error and bricked the whole
// chain. A regression back to fatal would have to delete this case and fail here.
func TestPlanHeightRepair(t *testing.T) {
	tests := []struct {
		name                         string
		proHeight, innerHeight, fork uint64
		want                         repairAction
	}{
		{"heights match => nothing", 8, 8, 0, repairNone},
		{"heights match at genesis", 0, 0, 0, repairNone},

		// The bug-3 case: proposervm index behind the inner tip must self-heal
		// (reset + re-bootstrap), never fatally brick init.
		{"behind by one (devnet-C 7<8)", 7, 8, 0, repairResetBehindIndex},
		{"behind by many", 100, 4242, 10, repairResetBehindIndex},
		{"behind, fork above inner still resets", 5, 9, 20, repairResetBehindIndex},

		// Proposervm ahead: roll back to the inner height when the target is
		// at/above the fork.
		{"ahead, fork below inner => rollback", 9, 7, 3, repairRollBackToInner},
		{"ahead by one, fork at genesis", 9, 8, 0, repairRollBackToInner},
		{"ahead, fork equals inner => rollback", 9, 7, 7, repairRollBackToInner},

		// Proposervm ahead but the rollback target is below the fork => forget
		// all proposervm indices (pre-fork territory).
		{"ahead, fork above inner => forget past fork", 9, 5, 7, repairForgetPastFork},
		{"ahead, rollback below fork", 100, 10, 11, repairForgetPastFork},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planHeightRepair(tt.proHeight, tt.innerHeight, tt.fork)
			if got != tt.want {
				t.Fatalf("planHeightRepair(%d,%d,%d) = %d, want %d",
					tt.proHeight, tt.innerHeight, tt.fork, got, tt.want)
			}
		})
	}

	// Explicit safety assertion: the behind-index case must NEVER resolve to a
	// no-op or a rollback (both of which would leave the node inconsistent or,
	// as before, crash) — it must always be the reset action.
	if got := planHeightRepair(7, 8, 0); got != repairResetBehindIndex {
		t.Fatalf("behind-index (7<8) must self-heal via repairResetBehindIndex, got %d", got)
	}
}
