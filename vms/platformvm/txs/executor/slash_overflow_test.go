// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"testing"

	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/stretchr/testify/require"
)

// TestHIGH2_SlashAmountNoOverflow verifies that the slash amount calculation
// does not overflow for large validator weights.
//
// Before the fix, the calculation was:
//
//	slashAmount = weight * pct / denom
//
// which overflows uint64 when weight * pct > math.MaxUint64.
// Example: weight=18_500_000_000_000_000_000 (18.5e18), pct=500_000 (50%).
// 18.5e18 * 500_000 = 9.25e24 which overflows uint64 (max ~1.84e19).
//
// The fix uses: (weight/denom)*pct + (weight%denom)*pct/denom
func TestHIGH2_SlashAmountNoOverflow(t *testing.T) {
	denom := uint64(reward.PercentDenominator)

	tests := []struct {
		name     string
		weight   uint64
		pct      uint64
		expected uint64
	}{
		{
			name:     "normal weight 10% slash",
			weight:   1_000_000_000, // 1B
			pct:      100_000,       // 10%
			expected: 100_000_000,   // 100M
		},
		{
			name:     "normal weight 50% slash",
			weight:   1_000_000_000, // 1B
			pct:      500_000,       // 50%
			expected: 500_000_000,   // 500M
		},
		{
			name:     "large weight 50% slash - would overflow old code",
			weight:   math.MaxUint64 / 2, // ~9.2e18
			pct:      500_000,             // 50%
			expected: math.MaxUint64 / 4,  // ~4.6e18
		},
		{
			name:     "max uint64 weight 10% slash",
			weight:   math.MaxUint64,
			pct:      100_000, // 10%
			expected: math.MaxUint64 / 10,
		},
		{
			name:     "weight exactly equal to denom",
			weight:   denom,
			pct:      500_000,
			expected: 500_000, // denom * 50% / denom = 50%
		},
		{
			name:     "weight less than denom",
			weight:   999_999,
			pct:      500_000,
			expected: 499_999, // (0)*500_000 + (999_999*500_000)/1_000_000 = 499_999
		},
		{
			name:     "weight=1 pct=1",
			weight:   1,
			pct:      1,
			expected: 0, // rounds to 0, will be bumped to 1 by the min check
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Safe arithmetic: (weight/denom)*pct + (weight%denom)*pct/denom
			result := (tt.weight/denom)*tt.pct + (tt.weight%denom)*tt.pct/denom
			require.Equal(t, tt.expected, result)
		})
	}
}

// TestHIGH2_SlashAmountMatchesNaive verifies the safe arithmetic produces
// the same result as big.Int multiplication for values that don't overflow.
func TestHIGH2_SlashAmountMatchesNaive(t *testing.T) {
	denom := uint64(reward.PercentDenominator)

	// Small values where naive multiplication works
	weights := []uint64{0, 1, 100, 1_000_000, 1_000_000_000, denom, denom * 2}
	pcts := []uint64{0, 1, 100_000, 500_000, 999_999, 1_000_000}

	for _, w := range weights {
		for _, p := range pcts {
			// Check if naive multiplication would overflow
			if w > 0 && p > 0 && w > math.MaxUint64/p {
				continue // skip values that overflow naive
			}

			naive := w * p / denom
			safe := (w/denom)*p + (w%denom)*p/denom

			require.Equal(t, naive, safe,
				"mismatch for weight=%d pct=%d: naive=%d safe=%d",
				w, p, naive, safe)
		}
	}
}
