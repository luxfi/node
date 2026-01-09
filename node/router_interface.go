// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"

	"github.com/luxfi/consensus/networking/handler"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/proto/pb/p2p"
	"github.com/luxfi/node/version"
	"github.com/luxfi/timer"
)

// Router handles message routing between chains
type Router interface {
	Initialize(
		nodeID ids.NodeID,
		logger log.Logger,
		timeoutManager timer.AdaptiveTimeoutManager,
		gossipFrequency uint64,
		harshQuittersTime uint64,
		harshQuittersSlashingFraction uint64,
		appGossipValidatorSize uint64,
		appGossipNonValidatorSize uint64,
		gossipAcceptedFrontierSize uint64,
		appSendQueueSize uint64,
		peerNotConnectedF uint64,
		connectedPeers ...ids.NodeID,
	) error

	RegisterRequest(
		ctx context.Context,
		nodeID ids.NodeID,
		chainID ids.ID,
		requestID uint32,
		op message.Op,
		failedMsg message.InboundMessage,
		engineType p2p.EngineType,
	)

	HandleInbound(ctx context.Context, msg message.InboundMessage)
	Shutdown(ctx context.Context)
	AddChain(ctx context.Context, chainID ids.ID, handler handler.Handler)
	Connected(nodeID ids.NodeID, nodeVersion *version.Application, netID ids.ID)
	Disconnected(nodeID ids.NodeID)
	Benched(chainID ids.ID, nodeID ids.NodeID)
	Unbenched(chainID ids.ID, nodeID ids.NodeID)
	HealthCheck(ctx context.Context) (interface{}, error)
	Deprecated() // Required for router.Router compatibility
}
