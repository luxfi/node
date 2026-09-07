// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/stretchr/testify/require"
)

// --- 1. an Ancestry response is a trust boundary, verified as a unit ---

// Each case is a response a peer can send that is not one contiguous ascending chain
// ENDING at the block asked about. Every one of them was believed before, because the
// walk indexed refs first and checked nothing.
//
// The wire order is OLDEST-FIRST (GetAncestors frames blocks ending at the requested
// one), so the requested block is last and its deepest ancestor is first.
func TestVerifyAncestryChunkRefusesMalformedResponses(t *testing.T) {
	refs, _ := refChain(5) // heights 0..5, refs[i].Height == i
	top := refs[5]

	// The honest response for top, oldest-first: 3, 4, 5.
	honest := []BlockRef{refs[3], refs[4], refs[5]}

	other, _ := refChain(5)

	cases := []struct {
		name string
		root ids.ID
		refs []BlockRef
		want error
	}{
		{"empty", top.ID, nil, ErrAncestryEmpty},
		{
			// Answers about a block we did not ask about, so the anchor's stake would be
			// credited to a chain of the peer's choosing.
			"wrong root", top.ID, []BlockRef{other[4], other[5]}, ErrAncestryWrongRoot,
		},
		{
			// Descending instead of ascending: the requested block is not last.
			"reversed order", top.ID,
			[]BlockRef{refs[5], refs[4], refs[3]}, ErrAncestryWrongRoot,
		},
		{
			// Two chains interleaved: not one path, so both would be credited.
			"not contiguous", top.ID,
			[]BlockRef{other[4], refs[5]}, ErrAncestryNotContiguous,
		},
		{
			// A parent-linked pair whose heights skip, so the descent is not what it claims.
			"height skips", top.ID,
			[]BlockRef{{ID: refs[4].ID, Height: 2, Parent: refs[3].ID}, refs[5]}, ErrAncestryHeightStep,
		},
		{
			// An id repeated inside one response.
			"cycle", top.ID,
			[]BlockRef{refs[3], refs[3], refs[4], refs[5]}, ErrAncestryCycle,
		},
		{
			// Height 0 is genesis; a parent there is spurious.
			"genesis claims a parent", refs[0].ID,
			[]BlockRef{{ID: refs[0].ID, Height: 0, Parent: ids.GenerateTestID()}},
			ErrAncestryHeightStep,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := verifyAncestryChunk(c.root, c.refs, nil)
			require.ErrorIs(t, err, c.want)
		})
	}

	// The honest response passes, or the rules above are simply rejecting everything.
	chunk, err := verifyAncestryChunk(top.ID, honest, nil)
	require.NoError(t, err)
	require.Equal(t, top.ID, chunk.Root)
	require.Len(t, chunk.Blocks, 3)
	require.Equal(t, refs[2].ID, chunk.Next, "Next is the parent of the DEEPEST block served")
	require.False(t, chunk.Complete)
}

// One id is one block. A second, different answer for an id already believed means a
// peer is lying and we cannot tell which, so neither answer is taken.
func TestVerifyAncestryChunkRefusesConflictingMetadata(t *testing.T) {
	refs, _ := refChain(5)
	index := map[ids.ID]BlockRef{refs[4].ID: refs[4]}
	known := func(id ids.ID) (BlockRef, bool) {
		ref, ok := index[id]
		return ref, ok
	}

	// Same id, different parent than the one already indexed. refs[4] is the parent of
	// refs[5], so a forged refs[4] cannot also be contiguous — assert on the whole chain
	// with only the metadata changed, which is exactly the lie a peer would tell.
	lying := []BlockRef{{ID: refs[4].ID, Height: 4, Parent: ids.GenerateTestID()}, refs[5]}
	_, err := verifyAncestryChunk(refs[5].ID, lying, known)
	require.ErrorIs(t, err, ErrAncestryConflict)

	// The consistent version of the same response is accepted.
	_, err = verifyAncestryChunk(refs[5].ID, []BlockRef{refs[4], refs[5]}, known)
	require.NoError(t, err)
}

// Genesis terminates the descent: Next empty, Complete true.
func TestVerifyAncestryChunkMarksGenesisComplete(t *testing.T) {
	refs, _ := refChain(2)
	chunk, err := verifyAncestryChunk(refs[2].ID, []BlockRef{refs[0], refs[1], refs[2]}, nil)
	require.NoError(t, err)
	require.True(t, chunk.Complete)
	require.Equal(t, ids.Empty, chunk.Next)
}

// hostileAncestry serves a response that fails one rule, to prove the refusal reaches
// the naming decision rather than only the unit under test.
type hostileAncestry struct {
	honest *stubAncestry
	forge  func(tip ids.ID, refs []BlockRef) []BlockRef
}

func (h *hostileAncestry) Ancestry(ctx context.Context, tip ids.ID, max int) ([]BlockRef, error) {
	refs, err := h.honest.Ancestry(ctx, tip, max)
	if err != nil || len(refs) == 0 {
		return refs, err
	}
	return h.forge(tip, refs), nil
}

// A peer splicing a foreign block into an otherwise honest ancestry must not get that
// block — or anything after it — credited. The whole chunk is refused, so the walk
// cannot be steered onto a chain the peer chose.
func TestNameFrontierRefusesSplicedAncestry(t *testing.T) {
	const w uint64 = 100
	refs, byID := refChain(10)
	common := refs[10]
	prev := common
	for i := 0; i < 6; i++ {
		c := childRef(prev)
		byID[c.ID] = c
		prev = c
	}
	ahead := prev

	foreign, _ := refChain(3)
	beacons := nodeIDs(3)
	policy := &BootstrapPolicy{
		TrustedBeacons: equalBeacons(beacons, w),
		MinResponses:   3,
		NamingWindow:   4,
		Source: &hostileAncestry{
			honest: &stubAncestry{byID: byID},
			forge: func(_ ids.ID, got []BlockRef) []BlockRef {
				// Keep the LAST ref honest so the root check passes, then splice foreign
				// blocks in front of it — the shape that gets a chain of the peer's
				// choosing credited with the anchor's stake.
				return []BlockRef{foreign[1], foreign[2], got[len(got)-1]}
			},
		},
	}
	_, err := policy.AcceptsFrontier(context.Background(), []BeaconReply{
		reply(beacons[0], ahead.ID, w),
		reply(beacons[1], common.ID, w),
		reply(beacons[2], common.ID, w),
	})
	require.Error(t, err, "a spliced ancestry must never name a frontier")
	require.ErrorIs(t, err, ErrNoBootstrapQuorum,
		"nothing was credited, so this is a genuine disagreement rather than a spent budget")
}

// --- 2. a child request never outlives the operation containing it ---

func TestNamingBudgetClampsRequestInsideAttempt(t *testing.T) {
	// The shipped defaults, which were inverted: 12s per request inside a 3s walk.
	def := (&BootstrapPolicy{}).namingBudget()
	require.Less(t, def.Request, def.Attempt,
		"a request that may outlive its walk is a bound in name only")
	require.Equal(t, bootstrapRequestTimeout, def.Request)
	require.Equal(t, bootstrapAttemptTimeout, def.Attempt)

	// A misconfiguration is clamped rather than allowed or fatal: refusing to bootstrap
	// over a bad duration is worse than honouring the smaller of the two.
	bad := (&BootstrapPolicy{Budget: NamingBudget{Attempt: time.Second, Request: time.Minute}}).namingBudget()
	require.Equal(t, time.Second, bad.Request)

	// NamingTimeout is the older name for the attempt bound; existing config keeps working.
	old := (&BootstrapPolicy{NamingTimeout: 5 * time.Second}).namingBudget()
	require.Equal(t, 5*time.Second, old.Attempt)
	require.LessOrEqual(t, old.Request, old.Attempt)
}

// slowAncestry blocks until its context is done, so one silent peer would consume the
// whole walk if the per-request bound were not enforced.
type slowAncestry struct{ calls int }

func (s *slowAncestry) Ancestry(ctx context.Context, _ ids.ID, _ int) ([]BlockRef, error) {
	s.calls++
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestOneSilentPeerDoesNotConsumeTheWholeAttempt(t *testing.T) {
	const w uint64 = 100
	refs, _ := refChain(4)
	beacons := nodeIDs(3)
	src := &slowAncestry{}
	policy := &BootstrapPolicy{
		TrustedBeacons: equalBeacons(beacons, w),
		MinResponses:   3,
		Source:         src,
		Budget: NamingBudget{
			Attempt: 900 * time.Millisecond,
			Request: 100 * time.Millisecond,
		},
	}
	start := time.Now()
	_, err := policy.AcceptsFrontier(context.Background(), []BeaconReply{
		reply(beacons[0], refs[4].ID, w),
		reply(beacons[1], refs[3].ID, w),
		reply(beacons[2], refs[2].ID, w),
	})
	require.Error(t, err)
	require.Less(t, time.Since(start), 3*time.Second, "the attempt must stay bounded")
	require.Greater(t, src.calls, 1,
		"a silent peer costs ONE request timeout, so the walk still reaches other anchors")
}

// --- 3. the budget bounds one attempt; the cursor persists ---

// The recoverability property, stated directly: a gap far wider than one attempt's
// budget is crossed by repeated attempts, because each resumes where the last stopped.
// With a per-attempt ceiling and no cursor this loops forever at the same depth,
// and the node never crosses the gap.
func TestDescentResumesAcrossAttemptsUntilTheGapIsCrossed(t *testing.T) {
	const w uint64 = 100
	refs, byID := refChain(10)
	common := refs[10]
	prev := common
	for i := 0; i < 120; i++ { // a gap 120 deep
		c := childRef(prev)
		byID[c.ID] = c
		prev = c
	}
	ahead := prev

	beacons := nodeIDs(3)
	replies := []BeaconReply{
		reply(beacons[0], ahead.ID, w),
		reply(beacons[1], common.ID, w),
		reply(beacons[2], common.ID, w),
	}
	// The cursor lives HERE, outliving every policy built below — the whole point.
	progress := map[ids.ID]*NamingProgress{}
	src := &stubAncestry{byID: byID}

	newAttempt := func() *BootstrapPolicy {
		return &BootstrapPolicy{
			TrustedBeacons: equalBeacons(beacons, w),
			MinResponses:   3,
			NamingWindow:   8,
			Source:         src,
			Progress:       progress,
			// One attempt can see 16 blocks; the gap is 120.
			Budget: NamingBudget{MaxBlocks: 16, MaxRequests: 4},
		}
	}

	// The first attempt cannot possibly reach the common ancestor.
	_, err := newAttempt().AcceptsFrontier(context.Background(), replies)
	require.ErrorIs(t, err, ErrNamingIncomplete, "out of budget, not out of agreement")
	firstDepth := progress[ahead.ID].Traversed
	require.Positive(t, firstDepth, "the first attempt must record the ground it covered")
	require.NotEqual(t, ids.Empty, progress[ahead.ID].Cursor, "and where it stopped")

	// Each further attempt must get STRICTLY deeper, or retrying is repetition.
	var named *Frontier
	depth := firstDepth
	for i := 0; i < 40 && named == nil; i++ {
		f, err := newAttempt().AcceptsFrontier(context.Background(), replies)
		if err == nil {
			named = f
			break
		}
		require.ErrorIs(t, err, ErrNamingIncomplete)
		got := progress[ahead.ID].Traversed
		require.Greater(t, got, depth,
			"attempt %d made no progress: a budget that resets with a cursor that does not "+
				"is the same permanent ceiling it replaced", i+2)
		depth = got
	}
	require.NotNil(t, named, "the gap must eventually be crossed")
	require.Equal(t, common.ID, named.ID, "and the block named is the ⅔-backed common ancestor")
	require.Equal(t, common.Height, named.Height)
}

// A request budget bounds round trips independently, so a peer dribbling one block per
// response cannot turn a block budget into unbounded requests.
func TestRequestBudgetBoundsDribbledResponses(t *testing.T) {
	const w uint64 = 100
	refs, byID := refChain(10)
	prev := refs[10]
	for i := 0; i < 60; i++ {
		c := childRef(prev)
		byID[c.ID] = c
		prev = c
	}
	beacons := nodeIDs(3)
	counted := &countingAncestry{inner: &stubAncestry{byID: byID}}
	policy := &BootstrapPolicy{
		TrustedBeacons: equalBeacons(beacons, w),
		MinResponses:   3,
		NamingWindow:   1, // one block per response
		Source:         counted,
		Budget:         NamingBudget{MaxBlocks: 10_000, MaxRequests: 5},
	}
	_, err := policy.AcceptsFrontier(context.Background(), []BeaconReply{
		reply(beacons[0], prev.ID, w),
		reply(beacons[1], refs[10].ID, w),
		reply(beacons[2], refs[10].ID, w),
	})
	require.Error(t, err)
	require.LessOrEqual(t, counted.calls, 5, "the request budget must bind independently")
}

type countingAncestry struct {
	inner *stubAncestry
	calls int
}

func (c *countingAncestry) Ancestry(ctx context.Context, tip ids.ID, max int) ([]BlockRef, error) {
	c.calls++
	return c.inner.Ancestry(ctx, tip, max)
}

// Retained descent state is bounded: anchors nobody reports any more are dropped, and
// what remains is capped by keeping the descents that got furthest.
func TestPruneNamingProgress(t *testing.T) {
	stale, live := ids.GenerateTestID(), ids.GenerateTestID()
	progress := map[ids.ID]*NamingProgress{
		stale: {Anchor: stale, Traversed: 100, VerifiedRefs: make([]BlockRef, 100)},
		live:  {Anchor: live, Traversed: 10, VerifiedRefs: make([]BlockRef, 10)},
	}
	PruneNamingProgress(progress, map[ids.ID]StakeWeight{live: 1}, 1_000)
	require.NotContains(t, progress, stale, "an unreported anchor can never be credited again")
	require.Contains(t, progress, live)

	// Over the cap, the least-progressed reported anchor goes first.
	a, b := ids.GenerateTestID(), ids.GenerateTestID()
	progress = map[ids.ID]*NamingProgress{
		a: {Anchor: a, Traversed: 5, VerifiedRefs: make([]BlockRef, 5)},
		b: {Anchor: b, Traversed: 90, VerifiedRefs: make([]BlockRef, 90)},
	}
	PruneNamingProgress(progress, map[ids.ID]StakeWeight{a: 1, b: 1}, 90)
	require.NotContains(t, progress, a, "the shallowest descent loses the least progress")
	require.Contains(t, progress, b)
}

// A policy with no Progress map still works — per-attempt descent, which is correct but
// cannot cross a gap wider than one attempt. Pinned so the nil path is not a panic.
func TestNilProgressStillNamesWithinOneAttempt(t *testing.T) {
	const w uint64 = 100
	refs, byID := refChain(10)
	common := refs[10]
	prev := common
	for i := 0; i < 6; i++ {
		c := childRef(prev)
		byID[c.ID] = c
		prev = c
	}
	beacons := nodeIDs(3)
	policy := &BootstrapPolicy{
		TrustedBeacons: equalBeacons(beacons, w),
		MinResponses:   3,
		NamingWindow:   4,
		Source:         &stubAncestry{byID: byID},
		Progress:       nil,
	}
	f, err := policy.AcceptsFrontier(context.Background(), []BeaconReply{
		reply(beacons[0], prev.ID, w),
		reply(beacons[1], common.ID, w),
		reply(beacons[2], common.ID, w),
	})
	require.NoError(t, err)
	require.Equal(t, common.ID, f.ID)
}
