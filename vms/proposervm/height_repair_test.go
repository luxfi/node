// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import "testing"

// TestClassifyHeightRepair locks the init-time reconciliation policy between the
// proposervm finality index and the inner VM's accepted tip.
//
// The load-bearing case is heightBehind: proposervm BELOW the inner (e.g. the
// devnet-C "index 7 < inner 8" from a snapshot restored inconsistently across
// the proposervm and EVM databases). It MUST classify as heightBehind — which
// the caller turns into a LOUD, actionable fatal — and must NEVER be treated as
// heightMatch (a no-op that leaves the node inconsistent) or heightAhead (a
// rollback). It must in particular never be "self-healed" by dropping the
// finality pointer: that leaves proposervm.LastAccepted() in the inner-id
// namespace and permanently wedges bootstrap/catch-up/live at the inner tip.
// A regression back to that silent reset would have to reclassify this case and
// fail here.
func TestClassifyHeightRepair(t *testing.T) {
	tests := []struct {
		name                   string
		proHeight, innerHeight uint64
		want                   heightRelation
	}{
		{"heights match => nothing", 8, 8, heightMatch},
		{"heights match at genesis", 0, 0, heightMatch},

		// The bug-3 case: proposervm index behind the inner tip is unrecoverable
		// locally and must be a loud fatal, never a silent reset or a rollback.
		{"behind by one (devnet-C 7<8)", 7, 8, heightBehind},
		{"behind by many", 100, 4242, heightBehind},
		{"behind at genesis boundary", 0, 1, heightBehind},

		// Proposervm ahead: the inner rolled back; roll the proposervm back.
		{"ahead by one", 9, 8, heightAhead},
		{"ahead by many", 4242, 100, heightAhead},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyHeightRepair(tt.proHeight, tt.innerHeight)
			if got != tt.want {
				t.Fatalf("classifyHeightRepair(%d,%d) = %d, want %d",
					tt.proHeight, tt.innerHeight, got, tt.want)
			}
		})
	}

	// Explicit safety assertion: the behind-index case must be heightBehind (loud
	// fatal), NEVER heightMatch (silent inconsistency) or heightAhead (rollback).
	// This is the exact regression RED flagged: a silent finality-pointer reset
	// here wedges the node forever.
	if got := classifyHeightRepair(7, 8); got != heightBehind {
		t.Fatalf("behind-index (7<8) must classify heightBehind (loud fatal), got %d", got)
	}
}
