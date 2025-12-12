// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"fmt"
	
	"time"

	"github.com/luxfi/metric"
	"github.com/luxfi/log"

	"github.com/luxfi/genesis/pkg/genesis"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/message"
	consensuscore "github.com/luxfi/consensus/core"
	validators "github.com/luxfi/consensus/validator"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/version"
)

var _ ExternalHandler = (*testExternalHandler)(nil)

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
func (t *testExternalHandler) HandleInbound(_ context.Context, msg message.InboundMessage) {
	t.log.Info(
		"receiving message",
		"op", msg.Op(),
	)
}

func (t *testExternalHandler) Connected(nodeID ids.NodeID, version *version.Application, netID ids.ID) {
	t.log.Info(
		"connected",
		"nodeID", nodeID,
		"version", version,
		"netID", netID,
	)
}

func (t *testExternalHandler) HandleGossip(_ context.Context, nodeID ids.NodeID, msg []byte) {
	t.log.Info(
		"received gossip",
		"nodeID", nodeID,
		"size", len(msg),
	)
}

func (t *testExternalHandler) HandleTimeout(_ context.Context) {
	t.log.Info("timeout occurred")
}

func (t *testExternalHandler) Disconnected(nodeID ids.NodeID) {
	t.log.Info(
		"disconnected",
		"nodeID", nodeID,
	)
}

func (t *testExternalHandler) AppRequest(_ context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, appRequestBytes []byte) error {
	t.log.Info("AppRequest", "nodeID", nodeID, "requestID", requestID)
	return nil
}

func (t *testExternalHandler) AppRequestFailed(_ context.Context, nodeID ids.NodeID, requestID uint32, appErr *consensuscore.AppError) error {
	t.log.Info("AppRequestFailed", "nodeID", nodeID, "requestID", requestID)
	return nil
}

func (t *testExternalHandler) AppResponse(_ context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	t.log.Info("AppResponse", "nodeID", nodeID, "requestID", requestID)
	return nil
}

func (t *testExternalHandler) AppGossip(_ context.Context, nodeID ids.NodeID, appGossipBytes []byte) error {
	t.log.Info("AppGossip", "nodeID", nodeID)
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

	// If we want to be able to communicate with non-primary network nets, we
	// should register them here.
	trackedNets := make(set.Set[ids.ID])

	// Messages and connections are handled by the external handler.
	handler := &testExternalHandler{
		log: log,
	}
	metrics := metric.NewRegistry()
	cfg, err := NewTestNetworkConfig(
		metrics,
		constants.TestnetID,
		validators,
		trackedNets,
	)
	if err != nil {
		panic(fmt.Sprintf("failed to create test network config: %v", err))
		return
	}
	network, err := NewTestNetwork(
		log,
		metrics,
		cfg,
		handler,
	)
	if err != nil {
		log.Error(
			"failed to create test network",
			"error", err,
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
		"error", err,
	)
}
