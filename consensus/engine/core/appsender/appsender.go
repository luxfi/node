// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package appsender

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/set"
)

// AppSender sends application-level messages to VMs
type AppSender interface {
	// SendAppRequest sends an application-level request to the given nodes.
	// The VM corresponding to this AppSender may receive either:
	// * An AppResponse from nodeID with ID [requestID]
	// * An AppRequestFailed from nodeID with ID [requestID]
	// A nil return value guarantees that the VM corresponding to this AppSender
	// will receive exactly one of the above messages.
	SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, message []byte) error

	// SendAppResponse sends an application-level response to a request.
	// This response must be in response to an AppRequest that the VM corresponding
	// to this AppSender received from [nodeID] with ID [requestID].
	SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, message []byte) error

	// SendAppGossip sends an application-level gossip message.
	SendAppGossip(ctx context.Context, config SendConfig, message []byte) error

	// SendAppGossipSpecific sends an application-level gossip message to a set of peers.
	SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], message []byte) error

	// SendCrossChainAppRequest sends an application-level request to a specific chain.
	SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, message []byte) error

	// SendCrossChainAppResponse sends an application-level response to a specific chain.
	// This response must be in response to a CrossChainAppRequest that the VM corresponding
	// to this AppSender received from [chainID] with ID [requestID].
	SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, message []byte) error
}

// SendConfig configures how a message is sent
type SendConfig struct {
	// Validators are the validators to send the message to
	Validators set.Set[ids.NodeID]
	// NonValidators are the non-validators to send the message to
	NonValidators set.Set[ids.NodeID]
}