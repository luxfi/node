// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// bootstrap_retry_test.go — which failures end initial sync, and which are just a bad round.
//
// Initial sync is the ONLY path that can close a gap wider than one served window;
// the live catch-up path cannot, by construction. So a failure that ends initial
// sync permanently ends the node's ability to rejoin, and the distinction between
// "this round found nobody" and "this chain cannot be reached from here" is the
// difference between a pause and a brick.

package chains

import (
	"errors"
	"fmt"
	"testing"

	chainbootstrap "github.com/luxfi/consensus/engine/chain/bootstrap"
)

// TestAStalledRoundIsRetried is the failure this file exists for.
//
// ErrStalled says no peer served the ancestry THIS ROUND — a statement about one
// pass over a sampled set, not about the chain. The peers that could serve may be
// busy, mid-restart, or simply not the ones this round drew. Ending initial sync
// on it leaves the node with only the live catch-up path, which cannot close a
// deep gap, so it re-asks forever and never rejoins.
func TestAStalledRoundIsRetried(t *testing.T) {
	if !isRetryableBootstrapFailure(chainbootstrap.ErrStalled) {
		t.Fatal("a stalled round ended initial sync — the node keeps only the live catch-up path, " +
			"which cannot close a gap wider than one served window, so it can never rejoin")
	}
}

// TestAStalledRoundIsRetriedWhenWrapped: the caller sees whatever the bootstrapper
// wrapped it in, so the predicate must match on the sentinel rather than the value.
func TestAStalledRoundIsRetriedWhenWrapped(t *testing.T) {
	wrapped := fmt.Errorf("attempt 3: %w", chainbootstrap.ErrStalled)
	if !isRetryableBootstrapFailure(wrapped) {
		t.Fatal("a wrapped stalled round was not recognised — errors.Is must see through the wrap")
	}
}

// TestConnectivityFailuresStayRetryable: the two that were already retried, so a
// change to the predicate cannot quietly drop them.
func TestConnectivityFailuresStayRetryable(t *testing.T) {
	for _, err := range []error{
		chainbootstrap.ErrBeaconsUnreachable,
		chainbootstrap.ErrNoBeaconQuorum,
	} {
		if !isRetryableBootstrapFailure(err) {
			t.Fatalf("%v must stay retryable — it is a transient quorum condition", err)
		}
	}
}

// TestAGapTooDeepIsNotRetried is the other half, and it is the one that keeps this
// honest: retrying must not become "retry everything". A gap wider than the
// in-memory window needs a different mechanism, and re-running the same pass
// cannot produce one — so it is surfaced for the operator rather than looped on.
func TestAGapTooDeepIsNotRetried(t *testing.T) {
	if isRetryableBootstrapFailure(chainbootstrap.ErrGapTooLarge) {
		t.Fatal("a gap too deep for the window was retried — a retry cannot fix it, " +
			"and looping hides the one failure an operator has to act on")
	}
}

// TestAnUnknownFailureIsNotRetried: the predicate lists what it knows. An error it
// has never seen is surfaced, not swallowed into an infinite loop.
func TestAnUnknownFailureIsNotRetried(t *testing.T) {
	if isRetryableBootstrapFailure(errors.New("something nobody has classified")) {
		t.Fatal("an unclassified failure was retried forever instead of being surfaced")
	}
}
