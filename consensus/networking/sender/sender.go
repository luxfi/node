// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/validators"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network/p2p"
	"github.com/luxfi/node/subnets"
	"github.com/luxfi/node/utils/set"
)

// Sender sends consensus messages
type Sender interface {
	// SendGetStateSummaryFrontier requests a state summary frontier from a peer
	SendGetStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32)

	// SendStateSummaryFrontier sends a state summary frontier to a peer
	SendStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte)

	// SendGetAcceptedStateSummary requests an accepted state summary from a peer
	SendGetAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, heights []uint64)

	// SendAcceptedStateSummary sends accepted state summaries to a peer
	SendAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs []ids.ID)

	// SendGetAcceptedFrontier requests the accepted frontier from a peer
	SendGetAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32)

	// SendAcceptedFrontier sends the accepted frontier to a peer
	SendAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID)

	// SendGetAccepted requests the accepted containers from a peer
	SendGetAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID)

	// SendAccepted sends accepted container IDs to a peer
	SendAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID)

	// SendGet requests a container from a peer
	SendGet(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID)

	// SendGetAncestors requests ancestors from a peer
	SendGetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID)

	// SendPut sends a container to a peer
	SendPut(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte)

	// SendAncestors sends ancestors to a peer
	SendAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containers [][]byte)

	// SendPushQuery sends a push query to a peer
	SendPushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte, requestedHeight uint64)

	// SendPullQuery sends a pull query to a peer
	SendPullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID, requestedHeight uint64)

	// SendChits sends chits to a peer
	SendChits(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDAtHeight ids.ID, acceptedID ids.ID)

	// SendGossip sends a gossip message to a subset of peers
	SendGossip(ctx context.Context, container []byte)

	// SendAppRequest sends an app-specific request to a peer
	SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, message []byte) error

	// SendAppResponse sends an app-specific response to a peer
	SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, message []byte) error

	// SendAppGossip sends an app-specific gossip message
	SendAppGossip(ctx context.Context, message []byte) error

	// SendCrossChainAppRequest sends a cross-chain app request
	SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, message []byte) error

	// SendCrossChainAppResponse sends a cross-chain app response
	SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, message []byte) error
}

// ExternalSender sends messages to other nodes
type ExternalSender interface {
	Send(msg message.OutboundMessage, nodeSIDs ...ids.NodeID) []ids.NodeID
}

// Config configures the sender
type Config struct {
	ChainID       ids.ID
	SubnetID      ids.ID
	MsgCreator    message.Creator
	Sender        ExternalSender
	Validators    validators.Set
	EngineType    p2p.EngineType
	SubnetTracker subnets.Tracker
}