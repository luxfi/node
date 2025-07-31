// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package appsender

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/engine/core"
	appsenderpb "github.com/luxfi/node/proto/pb/appsender"
)

var _ core.AppSender = (*Client)(nil)

// Client is a gRPC client that implements AppSender
type Client struct {
	client appsenderpb.AppSenderClient
}

// NewClient creates a new gRPC app sender client
func NewClient(client appsenderpb.AppSenderClient) *Client {
	return &Client{
		client: client,
	}
}

// SendAppRequest implements AppSender
func (c *Client) SendAppRequest(ctx context.Context, nodeIDs []ids.NodeID, requestID uint32, msg []byte) error {
	nodeIDsBytes := make([][]byte, len(nodeIDs))
	for i, nodeID := range nodeIDs {
		nodeIDsBytes[i] = nodeID[:]
	}
	
	_, err := c.client.SendAppRequest(ctx, &appsenderpb.SendAppRequestMsg{
		NodeIds:   nodeIDsBytes,
		RequestId: requestID,
		Request:   msg,
	})
	return err
}

// SendAppResponse implements AppSender
func (c *Client) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	_, err := c.client.SendAppResponse(ctx, &appsenderpb.SendAppResponseMsg{
		NodeId:    nodeID[:],
		RequestId: requestID,
		Response:  msg,
	})
	return err
}

// SendAppError implements AppSender
func (c *Client) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	_, err := c.client.SendAppError(ctx, &appsenderpb.SendAppErrorMsg{
		NodeId:       nodeID[:],
		RequestId:    requestID,
		ErrorCode:    errorCode,
		ErrorMessage: errorMessage,
	})
	return err
}

// SendAppGossip implements AppSender
func (c *Client) SendAppGossip(ctx context.Context, msg []byte) error {
	_, err := c.client.SendAppGossip(ctx, &appsenderpb.SendAppGossipMsg{
		Msg: msg,
	})
	return err
}

// SendCrossChainAppRequest implements AppSender
func (c *Client) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	// CrossChain methods might not be implemented in the protobuf
	// Return nil for now
	return nil
}

// SendCrossChainAppResponse implements AppSender
func (c *Client) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	// CrossChain methods might not be implemented in the protobuf
	// Return nil for now
	return nil
}