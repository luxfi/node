// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/message"
	"github.com/luxfi/p2p"
)

var (
	ErrRequestPending = errors.New("request pending")
	ErrNoPeers        = errors.New("no peers")
)

// ResponseCallback is called upon receiving a response to a request
// issued by Client.
// Callers should check [err] to see whether the request failed or not.
type ResponseCallback func(
	ctx context.Context,
	nodeID ids.NodeID,
	responseBytes []byte,
	err error,
)

type Client struct {
	handlerIDStr  string
	handlerPrefix []byte
	router        *router
	sender        p2p.Sender
	options       *clientOptions
}

// RequestAny issues a request to an arbitrary node decided by Client.
// If a specific node needs to be requested, use Request instead.
// See Request for more docs.
func (c *Client) RequestAny(
	ctx context.Context,
	requestBytes []byte,
	onResponse ResponseCallback,
) error {
	sampled := c.options.nodeSampler.Sample(ctx, 1)
	if len(sampled) != 1 {
		return ErrNoPeers
	}

	nodeIDs := set.Of(sampled...)
	return c.Request(ctx, nodeIDs, requestBytes, onResponse)
}

// Request issues an arbitrary request to a node.
// [onResponse] is invoked upon an error or a response.
func (c *Client) Request(
	ctx context.Context,
	nodeIDs set.Set[ids.NodeID],
	requestBytes []byte,
	onResponse ResponseCallback,
) error {
	// Cancellation is removed from this context to avoid erroring unexpectedly.
	// SendRequest should be non-blocking and any error other than context
	// cancellation is unexpected.
	//
	// This guarantees that the router should never receive an unexpected
	// response.
	ctxWithoutCancel := context.WithoutCancel(ctx)

	c.router.lock.Lock()
	defer c.router.lock.Unlock()

	requestBytes = PrefixMessage(c.handlerPrefix, requestBytes)
	for nodeID := range nodeIDs {
		requestID := c.router.requestID
		if _, ok := c.router.pendingRequests[requestID]; ok {
			return fmt.Errorf(
				"failed to issue request with request id %d: %w",
				requestID,
				ErrRequestPending,
			)
		}

		targetNodeIDs := set.NewSet[ids.NodeID](1)
		targetNodeIDs.Add(nodeID)
		if err := c.sender.SendRequest(
			ctxWithoutCancel,
			targetNodeIDs,
			requestID,
			requestBytes,
		); err != nil {
			c.router.log.Error("unexpected error when sending message",
				log.Stringer("op", message.AppRequestOp),
				log.Stringer("nodeID", nodeID),
				log.Uint32("requestID", requestID),
				log.Err(err),
			)
			return err
		}

		c.router.pendingRequests[requestID] = pendingRequest{
			handlerID: c.handlerIDStr,
			callback:  onResponse,
		}
		c.router.requestID += 2
	}

	return nil
}

// Gossip sends a gossip message to a random set of peers.
func (c *Client) Gossip(
	ctx context.Context,
	config p2p.SendConfig,
	gossipBytes []byte,
) error {
	// Cancellation is removed from this context to avoid erroring unexpectedly.
	// SendGossip should be non-blocking and any error other than context
	// cancellation is unexpected.
	ctxWithoutCancel := context.WithoutCancel(ctx)

	return c.sender.SendGossip(
		ctxWithoutCancel,
		config,
		PrefixMessage(c.handlerPrefix, gossipBytes),
	)
}

// PrefixMessage prefixes the original message with the protocol identifier.
//
// Only gossip and request messages need to be prefixed.
// Response messages don't need to be prefixed because request ids are tracked
// which map to the expected response handler.
func PrefixMessage(prefix, msg []byte) []byte {
	messageBytes := make([]byte, len(prefix)+len(msg))
	copy(messageBytes, prefix)
	copy(messageBytes[len(prefix):], msg)
	return messageBytes
}
