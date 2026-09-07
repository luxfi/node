// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import "time"

// The proposervm block wire stores the timestamp as a bare int64 at
// offTimestamp, with no version byte and no block kind to distinguish its unit.
// It was written as Unix SECONDS until commit 45a3dcf changed both sides to
// MILLISECONDS to unblock sub-second block cadence.
//
// That change has no activation gate, so it forks the network in both
// directions: a node reading seconds as milliseconds sees 1970 and freezes the
// LP-181 epoch, and a node reading milliseconds as seconds sees the year ~58363
// and rejects the block as too far in the future.
//
// decodeTimestamp closes the reading half. The two units are separated by three
// orders of magnitude, so for any time a block can plausibly carry they cannot
// be confused:
//
//	milliUnitFloor as SECONDS  = year 5138  — no block predates the fix by that
//	milliUnitFloor as MILLIS   = 1973-03-03 — no block postdates it
//
// Anything below the floor is therefore seconds, anything at or above it is
// milliseconds, and every node that runs this code reads both encodings. Blocks
// are still WRITTEN as seconds (buildUnsignedBuffer callers), because a node on
// an older build cannot read milliseconds and switching the write unit is the
// half that needs a coordinated activation.
const milliUnitFloor int64 = 100_000_000_000

func decodeTimestamp(v int64) time.Time {
	// Negative values are pre-1970 in either unit and only arise from a
	// malformed or zero header; time.Unix handles them without special-casing,
	// and the caller's own bounds checks reject the result.
	if v >= milliUnitFloor || v <= -milliUnitFloor {
		return time.UnixMilli(v)
	}
	return time.Unix(v, 0)
}

// encodeTimestamp is the write unit: seconds, which every deployed build reads.
// Flipping this to UnixMilli is a consensus change and needs an activation
// timestamp; decodeTimestamp already accepts the result when it happens.
func encodeTimestamp(t time.Time) int64 {
	return t.Unix()
}
