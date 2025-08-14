// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/luxfi/consensus/engine/core"
	"github.com/luxfi/consensus/networking/router"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/node/genesis"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/log"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/version"
)

var _ router.ExternalHandler = (*testExternalHandler)(nil)

// Note: all of the external handler's methods are called on peer goroutines. It
// is possible for multiple concurrent calls to happen with different NodeIDs.
// However, a given NodeID will only be performing one call at a time.
type testExternalHandler struct {
	log log.Logger
}

// Note: HandleInbound will be called with raw P2P messages, the networking
// implementation does not implicitly register timeouts, so this handler is only
// called by messages explicitly sent by the peer. If timeouts are required,
// that must be handled by the user of this utility.
func (t *testExternalHandler) HandleInbound(_ context.Context, msg interface{}) {
	if message, ok := msg.(message.InboundMessage); ok {
		t.log.Info(
			"receiving message",
			zap.Stringer("op", message.Op()),
		)
	}
}

func (t *testExternalHandler) Connected(nodeID ids.NodeID, version *version.Application, subnetID ids.ID) {
	t.log.Info(
		"connected",
		zap.Stringer("nodeID", nodeID),
		zap.Stringer("version", version),
		zap.Stringer("subnetID", subnetID),
	)
}

func (t *testExternalHandler) Disconnected(nodeID ids.NodeID) {
	t.log.Info(
		"disconnected",
		zap.Stringer("nodeID", nodeID),
	)
}

func (t *testExternalHandler) AppRequest(_ context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, appRequestBytes []byte) error {
	t.log.Info("AppRequest", zap.Stringer("nodeID", nodeID), zap.Uint32("requestID", requestID))
	return nil
}

func (t *testExternalHandler) AppRequestFailed(_ context.Context, nodeID ids.NodeID, requestID uint32, appErr *core.AppError) error {
	t.log.Info("AppRequestFailed", zap.Stringer("nodeID", nodeID), zap.Uint32("requestID", requestID))
	return nil
}

func (t *testExternalHandler) AppResponse(_ context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	t.log.Info("AppResponse", zap.Stringer("nodeID", nodeID), zap.Uint32("requestID", requestID))
	return nil
}

func (t *testExternalHandler) AppGossip(_ context.Context, nodeID ids.NodeID, appGossipBytes []byte) error {
	t.log.Info("AppGossip", zap.Stringer("nodeID", nodeID))
	return nil
}

type testAggressiveValidatorManager struct {
	validators.Manager
}

func (*testAggressiveValidatorManager) Contains(ids.ID, ids.NodeID) bool {
	return true
}

func ExampleNewTestNetwork() {
	var log log.Logger

	// Needs to be periodically updated by the caller to have the latest
	// validator set
	validators := &testAggressiveValidatorManager{
		Manager: validators.NewManager(),
	}

	// If we want to be able to communicate with non-primary network subnets, we
	// should register them here.
	trackedSubnets := set.Set[ids.ID]{}

	// Messages and connections are handled by the external handler.
	handler := &testExternalHandler{
		log: log,
	}

	network, err := NewTestNetwork(
		log,
		constants.TestnetID,
		validators,
		trackedSubnets,
		handler,
	)
	if err != nil {
		log.Error(
			"failed to create test network",
			zap.Error(err),
		)
		return
	}

	// We need to initially connect to some nodes in the network before peer
	// gossip will enable connecting to all the remaining nodes in the network.
	bootstrappers := genesis.SampleBootstrappers(constants.TestnetID, 5)
	for _, bootstrapper := range bootstrappers {
		network.ManuallyTrack(bootstrapper.ID, bootstrapper.IP)
	}

	// Typically network.StartClose() should be called based on receiving a
	// SIGINT or SIGTERM. For the example, we close the network after 15s.
	go func() {
		time.Sleep(15 * time.Second)
		network.StartClose()
	}()

	// network.Send(...) and network.Gossip(...) can be used here to send
	// messages to peers.

	// Calling network.Dispatch() will block until a fatal error occurs or
	// network.StartClose() is called.
	err = network.Dispatch()
	log.Info(
		"network exited",
		zap.Error(err),
	)
}
