// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/proto/p2p"
)

// TestChitsRouterFlattenLosesAcceptedFrontier pins what the router actually
// hands a chain handler when a Chits arrives.
//
// A Chits carries four payload values: PreferredId, PreferredIdAtHeight,
// AcceptedId and AcceptedHeight. Both live responders populate all of them
// (chains/manager.go PushQuery and PullQuery each read VM.LastAccepted and the
// accepted block's height before building the message). GetContainerBytes is
// the only extraction the router performs — node/chain_router.go:151 assigns
// its result to handler.Message.Message and nothing else from the decoded
// message survives — so whatever this returns is the whole of what an engine
// can ever see.
//
// It returns PreferredId alone. The other three values are decoded off the wire
// and then dropped. This test exists so that stops being a comment and starts
// being a failing test the moment someone widens the extraction.
//
// Widening it is not the fix it looks like. A Chits carries a peer's UNSIGNED
// preference, and receivePollResponse refuses those outright on any chain that
// requires signed votes — the engine tallies authenticated votes and nothing
// else. Carrying the peer's accepted frontier further would only give a caller
// something it may not act on, and acting on it is the failure mode: an
// unsigned claim about how far a peer has accepted is not evidence.
func TestChitsRouterFlattenLosesAcceptedFrontier(t *testing.T) {
	require := require.New(t)

	var (
		chainID             = ids.GenerateTestID()
		preferredID         = ids.GenerateTestID()
		preferredIDAtHeight = ids.GenerateTestID()
		acceptedID          = ids.GenerateTestID()
		acceptedHeight      = uint64(9_000_001)
	)

	msg := &p2p.Chits{
		ChainId:             chainID[:],
		RequestId:           7,
		PreferredId:         preferredID[:],
		PreferredIdAtHeight: preferredIDAtHeight[:],
		AcceptedId:          acceptedID[:],
		AcceptedHeight:      acceptedHeight,
	}

	got := GetContainerBytes(msg)

	// Exactly one ID survives, and it is the preference.
	require.Len(got, 32, "router hands the handler one ID's worth of bytes")
	require.Equal(preferredID[:], got)

	// The peer's accepted frontier is unrecoverable downstream: neither the ID
	// nor the height appears anywhere in what the handler receives.
	require.NotContains(string(got), string(acceptedID[:]))
	require.NotContains(string(got), string(preferredIDAtHeight[:]))
}

// TestInboundChitsHasNoAcceptedHeightSlot records that the receive-side builder
// never modelled AcceptedHeight at all: InboundChits takes three IDs and no
// height, so a test cannot even construct an inbound Chits that carries the
// peer's accepted height. The send side does populate it. The asymmetry is the
// divergence.
func TestInboundChitsHasNoAcceptedHeightSlot(t *testing.T) {
	require := require.New(t)

	inbound := InboundChits(
		ids.GenerateTestID(),
		7,
		ids.GenerateTestID(),
		ids.GenerateTestID(),
		ids.GenerateTestID(),
		ids.GenerateTestNodeID(),
	)

	chits, ok := inbound.Message().(*p2p.Chits)
	require.True(ok)
	require.Zero(chits.AcceptedHeight, "inbound builder has no parameter for it")
}
