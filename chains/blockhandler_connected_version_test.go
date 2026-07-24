// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	version "github.com/luxfi/version"
	"github.com/luxfi/vm/chain"
)

// recordingConnector is a chain.ChainVM that records the version it is handed on
// Connected. Every other method comes from the embedded (nil) interface and is
// never invoked by the connect path.
type recordingConnector struct {
	chain.ChainVM

	mu         sync.Mutex
	called     bool
	gotVersion *version.Application
}

func (c *recordingConnector) Connected(_ context.Context, _ ids.NodeID, nodeVersion *chain.VersionInfo) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.called = true
	c.gotVersion = nodeVersion
	return nil
}

func (c *recordingConnector) Disconnected(context.Context, ids.NodeID) error { return nil }

// TestBlockHandlerConnectedWithVersionDeliversRealVersion is the regression
// guard for RED CRITICAL #1 (C-Chain nil-version panic).
//
// Before the fix blockHandler.Connected forwarded connector.Connected(ctx,
// nodeID, nil) — a hardcoded nil version. proposervm promotes Connected to the
// inner C-Chain VM (coreth), whose state-sync peer tracker compares peer
// versions; a nil version there dereferences nil (version.Application.Compare)
// and PANICS a state-syncing node (a fresh join, or a validator rejoining after
// falling behind — the launch's core invariant).
//
// The fix plumbs the REAL peer version through ConnectedWithVersion to the
// connector. This test drives the real blockHandler with a real version and
// asserts the connector receives that non-nil version — never nil.
func TestBlockHandlerConnectedWithVersionDeliversRealVersion(t *testing.T) {
	require := require.New(t)

	rc := &recordingConnector{}
	bh := newBlockHandler(
		nil,          // BlockBuilder — unused by the connect path
		rc,           // connector under test
		log.Noop(),   // logger
		nil,          // engine
		nil,          // net
		nil,          // msgCreator
		ids.Empty,    // chainID
		ids.Empty,    // networkID
		nil,          // beacons
		ids.NodeID{}, // selfNodeID
		false,        // expectsStakedBeacons
	)

	nodeID := ids.GenerateTestNodeID()
	peerVersion := &version.Application{Name: "lux", Major: 1, Minor: 36, Patch: 27}

	require.NoError(bh.ConnectedWithVersion(context.Background(), nodeID, peerVersion))

	rc.mu.Lock()
	defer rc.mu.Unlock()
	require.True(rc.called, "connector.Connected must be invoked")
	require.NotNil(rc.gotVersion, "connector must receive a NON-nil version (coreth dereferences it in state-sync)")
	require.Equal("lux", rc.gotVersion.Name)
	require.Equal(1, rc.gotVersion.Major)
	require.Equal(36, rc.gotVersion.Minor)
	require.Equal(27, rc.gotVersion.Patch)
}

// TestBlockHandlerConnectedDedupsButStillCarriesVersion confirms the version
// survives the once-only dedup: the first (versioned) dispatch reaches the
// connector; a duplicate is a no-op (not a nil-version overwrite).
func TestBlockHandlerConnectedDedupsButStillCarriesVersion(t *testing.T) {
	require := require.New(t)

	rc := &recordingConnector{}
	bh := newBlockHandler(nil, rc, log.Noop(), nil, nil, nil, ids.Empty, ids.Empty, nil, ids.NodeID{}, false)

	nodeID := ids.GenerateTestNodeID()
	peerVersion := &version.Application{Name: "lux", Major: 1, Minor: 36, Patch: 27}

	require.NoError(bh.ConnectedWithVersion(context.Background(), nodeID, peerVersion))
	// A duplicate connect (e.g. dispatched again per tracked network) must be a
	// no-op — never a second call that could clobber the stored version with nil.
	require.NoError(bh.ConnectedWithVersion(context.Background(), nodeID, nil))

	rc.mu.Lock()
	defer rc.mu.Unlock()
	require.NotNil(rc.gotVersion, "the deduped duplicate must not overwrite the real version with nil")
	require.Equal(27, rc.gotVersion.Patch)
}
