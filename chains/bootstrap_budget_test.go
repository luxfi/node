// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import "testing"

// TestNamingBudget_RequestStrictlyInsideAttempt pins the ordering NamingBudget documents: a
// per-request bound must sit strictly inside the per-attempt budget that contains it. The
// descent walks ancestry in bootstrapNamingWindow-sized chunks, one Ancestry call per chunk, so
// a request permitted to consume the whole attempt starves every later chunk — the walk dies
// inside its first fetch and reports no quorum, which is the wedge the chunking exists to fix.
//
// namingBudget() already clamps Request down to Attempt rather than erroring, so a
// misconfiguration cannot prevent bootstrap. This test guards the DEFAULTS: a clamp that has to
// fire on the shipped constants would silently collapse the two bounds into one and take the
// chunked descent with it.
func TestNamingBudget_RequestStrictlyInsideAttempt(t *testing.T) {
	var p BootstrapPolicy
	b := p.namingBudget()

	if b.Request >= b.Attempt {
		t.Fatalf("default per-request bound (%v) must be strictly below the per-attempt budget (%v)",
			b.Request, b.Attempt)
	}
	// The clamp must be inert on the defaults — if it fires here, the shipped numbers are the
	// misconfiguration it exists to survive.
	if b.Request > bootstrapRequestTimeout {
		t.Fatalf("the Request clamp fired on default configuration (%v > %v): the shipped constants "+
			"themselves are inverted", b.Request, bootstrapRequestTimeout)
	}
	// One attempt must fit a useful number of SEQUENTIAL chunks, not just two. At 5 chunks and a
	// 256-block window that is >1000 blocks of halt-skew per attempt, with the persisted cursor
	// turning further attempts into progress rather than repetition.
	const minChunks = 5
	if got := b.Attempt / b.Request; got < minChunks {
		t.Fatalf("one attempt fits only %d sequential chunk fetches (%v / %v); need >= %d",
			got, b.Attempt, b.Request, minChunks)
	}
	if b.MaxRequests < minChunks {
		t.Fatalf("MaxRequests (%d) must permit at least %d chunks, else the request cap binds before "+
			"the time budget and the block budget can never be spent", b.MaxRequests, minChunks)
	}

	// The legacy package constants predate NamingBudget and still drive the raw Ancestors
	// transport (the fetch timer and the GetAncestors wire deadline). They must not contradict
	// the policy they were superseded by — two live numbers for one bound is how the original
	// 12s-request-inside-a-3s-walk inversion survived review.
	if bootstrapAncestorsTimeout > bootstrapNamingTimeout {
		t.Fatalf("legacy transport bounds are inverted: per-request %v exceeds per-attempt %v",
			bootstrapAncestorsTimeout, bootstrapNamingTimeout)
	}
	if bootstrapAncestorsTimeout != b.Request {
		t.Fatalf("the raw Ancestors fetch timer (%v) disagrees with the resolved policy Request bound "+
			"(%v); the transport would wait a different time than the walk budgeted for it",
			bootstrapAncestorsTimeout, b.Request)
	}
	if bootstrapNamingTimeout != b.Attempt {
		t.Fatalf("the legacy naming timeout (%v) disagrees with the resolved policy Attempt budget "+
			"(%v); BootstrapPolicy.namingTimeout() still returns the legacy value when NamingTimeout "+
			"is unset, so the two paths would disagree about one bound",
			bootstrapNamingTimeout, b.Attempt)
	}
}
