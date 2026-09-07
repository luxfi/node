// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/networking/handler"
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/version"
	luxversion "github.com/luxfi/version"
)

// fakeVersionedHandler implements handler.Handler AND the versionedConnector
// capability, recording which path the router used and the version delivered.
type fakeVersionedHandler struct {
	mu            sync.Mutex
	versionedHit  bool
	plainHit      bool
	gotAppVersion *luxversion.Application
}

func (h *fakeVersionedHandler) HandleInbound(context.Context, handler.Message) error  { return nil }
func (h *fakeVersionedHandler) HandleOutbound(context.Context, handler.Message) error { return nil }

func (h *fakeVersionedHandler) Connected(context.Context, ids.NodeID) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.plainHit = true
	return nil
}

func (h *fakeVersionedHandler) Disconnected(context.Context, ids.NodeID) error { return nil }

func (h *fakeVersionedHandler) ConnectedWithVersion(_ context.Context, _ ids.NodeID, v *luxversion.Application) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.versionedHit = true
	h.gotAppVersion = v
	return nil
}

// TestChainRouterConnectedDeliversConvertedVersion is the router half of that
// rule: chainRouter.Connected must deliver the REAL peer version to a
// version-capable handler, converting it from the node's peer version type
// (github.com/luxfi/node/version) to the VM boundary type
// (github.com/luxfi/version, aka chain.VersionInfo). Dispatching through the
// plain h.Connected(ctx, nodeID) drops the version; routing through the
// versionedConnector capability is what carries it across the boundary.
func TestChainRouterConnectedDeliversConvertedVersion(t *testing.T) {
	require := require.New(t)

	h := &fakeVersionedHandler{}
	chainID := ids.GenerateTestID()

	r := &chainRouter{
		log:            log.Noop(),
		chains:         map[ids.ID]handler.Handler{chainID: h},
		connectedPeers: set.NewSet[ids.NodeID](1),
	}

	nodeID := ids.GenerateTestNodeID()
	peerVersion := &version.Application{Name: "lux", Major: 1, Minor: 36, Patch: 27}

	r.Connected(nodeID, peerVersion, constants.PrimaryNetworkID)

	h.mu.Lock()
	defer h.mu.Unlock()
	require.True(h.versionedHit, "router must use the versioned capability path for a version-capable handler")
	require.False(h.plainHit, "router must NOT fall back to the nil-version plain Connected")
	require.NotNil(h.gotAppVersion, "handler must receive a non-nil converted version")
	require.Equal("lux", h.gotAppVersion.Name)
	require.Equal(1, h.gotAppVersion.Major)
	require.Equal(36, h.gotAppVersion.Minor)
	require.Equal(27, h.gotAppVersion.Patch)
}

// TestToAppVersionNilSafe documents that a nil peer version converts to nil
// (never a panic) — the conversion is defensive at the boundary.
func TestToAppVersionNilSafe(t *testing.T) {
	require.Nil(t, toAppVersion(nil))
}
