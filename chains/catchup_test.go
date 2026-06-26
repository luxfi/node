// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"testing"

	"github.com/luxfi/ids"
	"github.com/stretchr/testify/require"
)

// *blockHandler must satisfy ancestorRequester, or the catch-up wire silently
// loses its transport. Catch it at compile time, not in production.
var _ ancestorRequester = (*blockHandler)(nil)

// catchupSpy records the requestContext calls networkCatchup bridges to.
type catchupSpy struct {
	calls     int
	lastFrom  ids.NodeID
	lastBlock ids.ID
}

func (s *catchupSpy) requestContext(_ context.Context, from ids.NodeID, blockID ids.ID) {
	s.calls++
	s.lastFrom = from
	s.lastBlock = blockID
}

// TestNetworkCatchup_BridgesEngineSignalToWire is the regression for the
// stranded-follower bug: node never set netCfg.Catchup, so the engine's
// requestCatchup hit a nil interface and a follower that fell behind during
// consensus never fetched the missing ancestors — it looped on an unfinalizable
// orphan forever (observed live: mainnet luxd-0/2 stuck at 1082780 while peers
// reached 1082793). The wire must (a) be nil-safe before its handler is
// late-bound and (b) route the engine's RequestAncestors to the handler's
// GetAncestors transport (requestContext), once, for the right block and peer.
func TestNetworkCatchup_BridgesEngineSignalToWire(t *testing.T) {
	missing := ids.GenerateTestID()
	peer := ids.GenerateTestNodeID()

	// (a) Before late-binding (handler nil): a harmless no-op, never a panic.
	c := &networkCatchup{}
	require.NoError(t, c.RequestAncestors(ids.Empty, ids.Empty, missing, peer))

	// (b) Once wired: the engine's catch-up signal reaches the GetAncestors wire
	// exactly once — for the missing block, addressed to the peer that advertised
	// its child. RED before the fix: handler is never set, calls stays 0.
	spy := &catchupSpy{}
	c.handler = spy
	require.NoError(t, c.RequestAncestors(ids.Empty, ids.Empty, missing, peer))
	require.Equal(t, 1, spy.calls, "RequestAncestors must route to requestContext — a nil wire IS the stranded-follower bug")
	require.Equal(t, missing, spy.lastBlock)
	require.Equal(t, peer, spy.lastFrom)
}
