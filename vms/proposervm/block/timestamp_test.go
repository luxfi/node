// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/stretchr/testify/require"
)

// Blocks written by a build from before 45a3dcf carry seconds. Blocks written by
// 45a3dcf..HEAD carry milliseconds. A node must read both, or a rolling upgrade
// halts the chain: seconds read as millis is 1970, millis read as seconds is the
// year ~58363 and fails the too-far-in-the-future bound.
func TestDecodeTimestampReadsBothUnits(t *testing.T) {
	// Every date a block can plausibly carry, across both units.
	for _, want := range []time.Time{
		time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC), // the day 45a3dcf landed
		time.Date(2030, 6, 15, 12, 30, 45, 0, time.UTC),
		time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(5000, 1, 1, 0, 0, 0, 0, time.UTC),
	} {
		t.Run(want.Format("2006-01-02"), func(t *testing.T) {
			require.Equal(t, want.UTC(), decodeTimestamp(want.Unix()).UTC(),
				"a seconds-encoded block must read back as itself")
			require.Equal(t, want.UTC(), decodeTimestamp(want.UnixMilli()).UTC(),
				"a millis-encoded block must read back as itself")
		})
	}
}

// The unit is inferred from magnitude, so the inference must be exact at the
// boundary rather than approximately right near it.
func TestDecodeTimestampUnitBoundary(t *testing.T) {
	require.Equal(t, time.Unix(milliUnitFloor-1, 0).UTC(),
		decodeTimestamp(milliUnitFloor-1).UTC(),
		"just below the floor is seconds")
	require.Equal(t, time.UnixMilli(milliUnitFloor).UTC(),
		decodeTimestamp(milliUnitFloor).UTC(),
		"at the floor is milliseconds")
}

// The floor is only safe because neither reading of it is a time a block can
// carry. If someone moves it, this fails and says why.
func TestUnitFloorIsOutsideEveryPlausibleBlockTime(t *testing.T) {
	asSeconds := time.Unix(milliUnitFloor, 0).UTC()
	asMillis := time.UnixMilli(milliUnitFloor).UTC()

	require.Greater(t, asSeconds.Year(), 3000,
		"read as seconds the floor must be far future, so no real block reaches it")
	require.Less(t, asMillis.Year(), 1990,
		"read as millis the floor must predate the chain, so no real block is below it")
}

// Blocks are written in seconds: a node on an older build cannot read
// milliseconds, and changing the write unit is the half that needs a
// coordinated activation. This is the assertion that fails when someone flips
// it without one.
func TestBlocksAreWrittenInSecondsUntilActivation(t *testing.T) {
	ts := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	require.Equal(t, ts.Unix(), encodeTimestamp(ts),
		"the write unit is seconds; flipping it to UnixMilli is a consensus "+
			"change and needs an activation timestamp")
}

// Sub-second detail is lost by the seconds write unit. Stated as a test so the
// cost is recorded rather than discovered: this is what an activation buys back.
func TestSecondsWriteUnitTruncatesSubSecond(t *testing.T) {
	ts := time.Date(2026, 7, 29, 12, 0, 0, 500_000_000, time.UTC)
	require.Equal(t, ts.Truncate(time.Second).UTC(),
		decodeTimestamp(encodeTimestamp(ts)).UTC())
}

// The round trip a proposer and its peers actually perform.
func TestBuildUnsignedTimestampRoundTrip(t *testing.T) {
	ts := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	blk, err := BuildUnsigned(ids.Empty, ts, 1_000, Epoch{}, []byte("body"))
	require.NoError(t, err)
	require.Equal(t, ts.UTC(), blk.Timestamp().UTC())

	parsed, err := Parse(blk.Bytes(), ids.Empty)
	require.NoError(t, err)
	signed, ok := parsed.(SignedBlock)
	require.True(t, ok, "an unsigned proposervm block still parses as SignedBlock")
	require.Equal(t, ts.UTC(), signed.Timestamp().UTC(),
		"a parsed block must carry the timestamp its proposer set")
}
