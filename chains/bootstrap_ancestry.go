// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/ids"
)

// Ancestry is served by a PEER, so an Ancestry response is a trust boundary and is
// validated as a UNIT before any of it is believed. The frontier tally walks parent
// links to credit stake, so a response that is not one contiguous descending chain
// from the block we asked about lets a peer inflate what its stake vouches for:
// interleave two forks and both get credited; repeat an id at two heights and the
// walk can be steered; answer about a block we did not ask about and the anchor's
// stake is attached to a chain of the peer's choosing.
//
// The previous loop indexed every returned ref first and checked nothing, so a
// malformed or hostile chunk was already believed by the time anyone could reject it.
// Partial belief is the whole defect: verification has to gate entry to the index,
// which means the chunk is the unit, not the ref.

var (
	// ErrAncestryEmpty: the peer served nothing. Not hostile on its own — the anchor
	// simply contributes no ancestry and the walk moves on.
	ErrAncestryEmpty = errors.New("bootstrap ancestry: empty response")
	// ErrAncestryWrongRoot: the response does not begin at the block requested, so it
	// answers a question we did not ask.
	ErrAncestryWrongRoot = errors.New("bootstrap ancestry: response does not start at the requested block")
	// ErrAncestryNotContiguous: some ref is not the parent of the one before it, so the
	// response is not a single chain.
	ErrAncestryNotContiguous = errors.New("bootstrap ancestry: refs do not form one contiguous parent path")
	// ErrAncestryHeightStep: a height does not decrease by exactly one, so a gap or a
	// repeat is hiding in the chain.
	ErrAncestryHeightStep = errors.New("bootstrap ancestry: height does not decrease by exactly one")
	// ErrAncestryCycle: an id repeats inside the chunk.
	ErrAncestryCycle = errors.New("bootstrap ancestry: id repeats within the response")
	// ErrAncestryConflict: an id already known carries different height or parent than
	// before. One id is one block; two answers for it means at least one peer is lying,
	// and we cannot tell which, so neither is believed.
	ErrAncestryConflict = errors.New("bootstrap ancestry: id served with conflicting height or parent")
)

// VerifiedAncestryChunk is one Ancestry response that passed every structural rule.
// It exists so that "verified" is a TYPE rather than a comment: a []BlockRef can be
// anything a peer sent, and only a VerifiedAncestryChunk may reach the index.
//
// WIRE ORDER IS OLDEST-FIRST. GetAncestors frames up to `max` blocks ENDING at the
// requested block, ascending, so the block asked about is the LAST element and the
// deepest ancestor served is the first. The rules below are written against that
// order, not against a guess about it.
type VerifiedAncestryChunk struct {
	// Root is the block requested, and the LAST element of Blocks.
	Root ids.ID
	// Blocks ascends: each is the child of the one before it, one height per step, no
	// id repeated, ending at Root.
	Blocks []BlockRef
	// Next is the parent of Blocks[0], the deepest block served — where the descent
	// continues.
	Next ids.ID
	// Complete is true when Next is empty: genesis is reached and there is no more
	// ancestry to ask for.
	Complete bool
}

// verifyAncestryChunk validates a served response against the block it answers and
// the blocks already believed, returning a chunk only if every rule holds.
//
// known resolves an id already in the index; it is consulted for the conflict rule
// and may be nil.
func verifyAncestryChunk(root ids.ID, refs []BlockRef, known func(ids.ID) (BlockRef, bool)) (VerifiedAncestryChunk, error) {
	if len(refs) == 0 {
		return VerifiedAncestryChunk{}, ErrAncestryEmpty
	}
	// Oldest-first: the block we asked about is the LAST one served.
	if last := refs[len(refs)-1]; last.ID != root {
		return VerifiedAncestryChunk{}, fmt.Errorf("%w: asked %s, served %s last",
			ErrAncestryWrongRoot, root, last.ID)
	}

	seen := make(map[ids.ID]struct{}, len(refs))
	for i, ref := range refs {
		if _, dup := seen[ref.ID]; dup {
			return VerifiedAncestryChunk{}, fmt.Errorf("%w: %s", ErrAncestryCycle, ref.ID)
		}
		seen[ref.ID] = struct{}{}

		if i > 0 {
			prev := refs[i-1] // the PARENT, since the order ascends
			if ref.Parent != prev.ID {
				return VerifiedAncestryChunk{}, fmt.Errorf("%w: %s is not the child of %s",
					ErrAncestryNotContiguous, ref.ID, prev.ID)
			}
			if ref.Height != prev.Height+1 {
				return VerifiedAncestryChunk{}, fmt.Errorf("%w: %s at %d follows %s at %d",
					ErrAncestryHeightStep, ref.ID, ref.Height, prev.ID, prev.Height)
			}
		}

		// One id is one block. A second, different answer for an id we already hold
		// means a peer is lying and we cannot tell which, so the chunk is refused.
		if known != nil {
			if prior, ok := known(ref.ID); ok && (prior.Height != ref.Height || prior.Parent != ref.Parent) {
				return VerifiedAncestryChunk{}, fmt.Errorf(
					"%w: %s known at height %d parent %s, served at height %d parent %s",
					ErrAncestryConflict, ref.ID, prior.Height, prior.Parent, ref.Height, ref.Parent)
			}
		}
	}

	deepest := refs[0]
	// Genesis is height 0; nothing is served below it, and a claimed parent there would
	// be spurious.
	if deepest.Height == 0 && deepest.Parent != ids.Empty {
		return VerifiedAncestryChunk{}, fmt.Errorf("%w: %s at height 0 claims parent %s",
			ErrAncestryHeightStep, deepest.ID, deepest.Parent)
	}
	return VerifiedAncestryChunk{
		Root:     root,
		Blocks:   refs,
		Next:     deepest.Parent,
		Complete: deepest.Parent == ids.Empty,
	}, nil
}

// NamingProgress is one anchor's descent, carried ACROSS attempts.
//
// The depth budget it replaces was a RECOVERABILITY CEILING: a fleet whose straggler
// sat further below the highest tip than the budget allowed could never resolve its
// common ancestor, however many times it retried, because every attempt restarted the
// descent from the tip and ran out at the same place. A budget that resets and a
// cursor that does not are different things; only the second one recovers.
//
// So a budget now bounds ONE ATTEMPT, and the cursor persists. An attempt that runs
// out saves where it stopped and reports incomplete; the next resumes from there and
// gets that much further down. Progress is monotonic, so an arbitrarily large gap is
// crossed in a bounded number of attempts with a bounded cost each.
type NamingProgress struct {
	// Anchor is the reported tip this descent started from.
	Anchor ids.ID
	// Cursor is the next block to request. Empty once the descent finished.
	Cursor ids.ID
	// Traversed counts verified blocks accumulated across every attempt.
	Traversed int
	// VerifiedRefs is every ref that passed verifyAncestryChunk, in descent order.
	// Held across attempts so the work already paid for is not paid for again.
	VerifiedRefs []BlockRef
	// Complete is true when the descent reached genesis, or a block another anchor's
	// chain already covers. A complete descent is never resumed.
	Complete bool
}

// resume reports the block the next request should ask about: the saved cursor when a
// previous attempt stopped part-way, otherwise the anchor.
func (n *NamingProgress) resume() ids.ID {
	if n.Cursor != ids.Empty {
		return n.Cursor
	}
	return n.Anchor
}

// NamingBudget bounds ONE attempt of the ancestor-tolerant walk. Every field is a
// resource bound and none of them is a safety property: safety comes from the
// verified chunk above, the distinct-voter floor, the agreement threshold over
// responder stake, and the full re-verify each block gets on the descent.
type NamingBudget struct {
	// MaxBlocks bounds verified blocks one attempt may add across all anchors, which
	// is also the memory bound — BlockRef is a fixed 72 bytes, so a separate byte
	// budget would be this same constraint stated twice.
	MaxBlocks int
	// MaxRequests bounds Ancestry calls one attempt may make, so a peer serving one
	// block per response cannot turn a block budget into unbounded round trips.
	MaxRequests int
	// Attempt bounds wall clock for the whole walk.
	Attempt time.Duration
	// Request bounds ONE Ancestry call and MUST be smaller than Attempt: a child
	// request that may outlive the operation containing it is a bound in name only,
	// and it was configured that way — 12s per request inside a 3s walk, so the walk's
	// deadline was the only real limit and the chunked descent it was meant to permit
	// could not finish a second chunk.
	Request time.Duration
}

const (
	// bootstrapMaxBlocksPerAttempt: one attempt indexes at most this many verified
	// blocks (~590 KiB of BlockRef). Crossing a larger gap takes more attempts, which
	// the persisted cursor makes progress rather than repetition.
	bootstrapMaxBlocksPerAttempt = 8192
	// bootstrapMaxRequestsPerAttempt: at the 256-block window this is 32 full chunks,
	// enough to spend the block budget; a peer dribbling one block per response
	// exhausts requests instead and the attempt ends.
	bootstrapMaxRequestsPerAttempt = 64
	// bootstrapAttemptTimeout bounds the whole walk. It is the loop's per-round cost,
	// paid only on the split path — a single tip clearing the floor takes the fast path
	// and fetches nothing.
	bootstrapAttemptTimeout = 30 * time.Second
	// bootstrapRequestTimeout bounds ONE Ancestry call, strictly inside the attempt.
	// An honest beacon that just answered the frontier serves nearby ancestry well
	// within it; a silent one costs this much and the walk moves to the next anchor.
	bootstrapRequestTimeout = 3 * time.Second
)

// namingBudget resolves the per-attempt budget, defaulting each field independently so
// a caller may override one without restating the others.
func (p *BootstrapPolicy) namingBudget() NamingBudget {
	b := p.Budget
	if b.MaxBlocks <= 0 {
		b.MaxBlocks = bootstrapMaxBlocksPerAttempt
	}
	if b.MaxRequests <= 0 {
		b.MaxRequests = bootstrapMaxRequestsPerAttempt
	}
	if b.Attempt <= 0 {
		// NamingTimeout is the older name for this bound and is honoured when set, so
		// existing configuration keeps working.
		if p.NamingTimeout > 0 {
			b.Attempt = p.NamingTimeout
		} else {
			b.Attempt = bootstrapAttemptTimeout
		}
	}
	if b.Request <= 0 {
		b.Request = bootstrapRequestTimeout
	}
	// A request may never outlive the attempt that contains it, whatever was
	// configured. Clamping rather than erroring keeps a misconfiguration from
	// preventing bootstrap, and the clamp is the honest reading of both numbers.
	if b.Request > b.Attempt {
		b.Request = b.Attempt
	}
	return b
}

// progressFor returns an anchor's saved descent, or nil when there is none.
func (p *BootstrapPolicy) progressFor(anchor ids.ID) *NamingProgress {
	if p.Progress == nil {
		return nil
	}
	return p.Progress[anchor]
}

// ensureProgress returns the anchor's descent, creating it on first sight. When the
// caller supplied no Progress map the descent is per-attempt: correct, but unable to
// cross a gap wider than one attempt's budget.
func (p *BootstrapPolicy) ensureProgress(anchor ids.ID) *NamingProgress {
	if p.Progress == nil {
		return &NamingProgress{Anchor: anchor}
	}
	if prog, ok := p.Progress[anchor]; ok {
		return prog
	}
	prog := &NamingProgress{Anchor: anchor}
	p.Progress[anchor] = prog
	return prog
}

// PruneNamingProgress drops descents for anchors no longer reported, and — if what
// remains still exceeds maxRefs — the least-progressed of those, until it fits.
//
// Retention is what crosses a wide gap, so it cannot simply be capped at one attempt's
// worth; but it cannot grow without limit either. Anchors nobody reports any more are
// pure cost: no tip's credit walk can reach their refs. Beyond that, keeping the
// descents that have got FURTHEST preserves the most recovery progress per byte.
func PruneNamingProgress(progress map[ids.ID]*NamingProgress, reported map[ids.ID]StakeWeight, maxRefs int) {
	if progress == nil {
		return
	}
	total := 0
	for anchor, prog := range progress {
		if _, still := reported[anchor]; !still {
			delete(progress, anchor)
			continue
		}
		total += len(prog.VerifiedRefs)
	}
	for total > maxRefs && len(progress) > 0 {
		var victim ids.ID
		fewest := -1
		for anchor, prog := range progress {
			if fewest < 0 || prog.Traversed < fewest {
				victim, fewest = anchor, prog.Traversed
			}
		}
		total -= len(progress[victim].VerifiedRefs)
		delete(progress, victim)
	}
}

// bootstrapMaxRetainedRefs bounds retained descent state across attempts: 262144 refs
// is ~18 MiB of BlockRef, enough to hold eight anchors' full descents over any halt
// skew a fleet has produced, and it is released the moment a frontier is named.
const bootstrapMaxRetainedRefs = 262144
