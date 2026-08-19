// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// red_unsolicited_ancestors_test.go — the reply lane answers to a number, not to a peer.
//
// A bootstrap fetch goes out to a sampled set of staked beacons. The reply comes back
// through handleContext, which knows who sent it, and is handed to
// deliverBootstrapAncestors, which does not take a sender at all. So the batch the
// descent works on is whichever framed payload carries the right requestID first —
// beacon or bystander. Past initial sync there is not even that much: pendingContext
// records the {nodeID, requestID} of every fetch this node sends, keyed by the block it
// wants, and nothing reads it back — the TTL reaper, the cap and the no-peer release
// clear it, a reply never does.
//
// A bootstrapper that indexes by id alone cannot express this:
// Ancestors() looks up {NodeID, RequestID} in outstandingRequests and drops anything
// that is not a pair it created.
//
// The consequence lives one layer up. consensus/engine/chain/bootstrap/bootstrapper.go
// abandons a pass whose batch does not contain the block it asked for, and 60 abandoned
// passes in a row is ErrStalled — which chains/bootstrap_sync.go
// isRetryableBootstrapFailure declines to retry, so the manager stops the engine.
// TestRED_ForgedAncestryUnderHonestFrontierRejected already pins that half: off-path
// ancestry finalizes nothing and stalls. This pins the half that lets an arbitrary peer
// supply it.
package chains

import (
	"encoding/binary"
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// frameAncestors wraps block payloads the way the Ancestors responder does, so the
// bootstrap lane decodes them as a real batch.
func frameAncestors(blocks ...[]byte) []byte {
	var out []byte
	for _, b := range blocks {
		entry := encodeCatchupEntry(b, nil)
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(entry)))
		out = append(out, length[:]...)
		out = append(out, entry...)
	}
	return out
}

// TestRED_UnsolicitedAncestorsFeedTheBootstrapDescent: a peer that was never sampled
// answers a bootstrap fetch, and the descent consumes its batch.
func TestRED_UnsolicitedAncestorsFeedTheBootstrapDescent(t *testing.T) {
	chain, _ := buildBSChain(4, -1)
	bh := &blockHandler{
		logger:       log.NewNoOpLogger(),
		vm:           newBSVMAt(chain, 0),
		bsAncestorCh: make(map[uint32]chan []bootstrapFetchedBlock),
	}
	bh.bsActive.Store(true)

	// What Ancestors() registers before sending GetAncestors to its beacon sample.
	const requestID = 7
	waiter := make(chan []bootstrapFetchedBlock, bootstrapAncestorSample)
	bh.bsAncestorCh[requestID] = waiter

	// Nobody asked this node for anything. It is not a beacon, not in the sample, and
	// on a default fleet (DefaultNetworkRequireValidatorToConnect is false) it does not
	// need stake to be connected and routed here.
	bystander := ids.GenerateTestNodeID()
	junk := frameAncestors([]byte("not a block"))

	// Ask the lane directly: whether an unasked peer's reply reaches the descent is a
	// property of the lane, and routing through handleContext would also drive the live
	// cert path, which this handler is not built for.
	if bh.deliverBootstrapAncestors(bystander, requestID, junk) {
		t.Fatalf("the bootstrap lane CLAIMED a reply from a peer it never asked")
	}

	select {
	case batch := <-waiter:
		t.Logf("the bootstrap descent is holding %d entr(ies) from a peer it never asked: %q",
			len(batch), batch[0].Bytes)
		t.Fatalf("an Ancestors reply from an unsampled peer reached the bootstrap descent — " +
			"the lane correlates on requestID alone, so any connected peer can answer a fetch " +
			"and abandon the pass (60 in a row is the terminal ErrStalled)")
	default:
		// Correct behavior: the reply names a peer this fetch never asked, so it is not
		// this lane's batch.
	}
}
