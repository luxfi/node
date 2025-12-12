// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
)

// WarpSender handles warp messaging including cross-chain communication.
// Method names use "App" prefix for compatibility with consensus layer.
type WarpSender interface {
	// SendAppRequest sends a request to the given nodes.
	SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error

	// SendAppResponse sends a response to a request.
	SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, responseBytes []byte) error

	// SendAppError sends an error response.
	SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error

	// SendAppGossip sends a gossip message.
	SendAppGossip(ctx context.Context, nodeIDs set.Set[ids.NodeID], gossipBytes []byte) error

	// SendAppGossipSpecific sends a gossip message to a specific set of nodes.
	SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], gossipBytes []byte) error

	// SendCrossChainAppRequest sends a cross-chain request.
	SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, requestBytes []byte) error

	// SendCrossChainAppResponse sends a cross-chain response.
	SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, responseBytes []byte) error

	// SendCrossChainAppError sends a cross-chain error.
	SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error
}
