package appsender

import (
	"context"

	"github.com/luxfi/consensus/core"
	"github.com/luxfi/consensus/utils/set"
	"github.com/luxfi/ids"
	appsenderpb "github.com/luxfi/node/proto/pb/appsender"
)

// NewClient creates a new app sender client
func NewClient(client appsenderpb.AppSenderClient) core.AppSender {
	return &Client{client: client}
}

// Client is an AppSender client
type Client struct {
	client appsenderpb.AppSenderClient
}

func (c *Client) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) error {
	// Convert set to slice of byte arrays for protobuf
	nodeIDBytes := make([][]byte, 0, nodeIDs.Len())
	for nodeID := range nodeIDs {
		nodeIDBytes = append(nodeIDBytes, nodeID[:])
	}
	
	_, err := c.client.SendAppRequest(ctx, &appsenderpb.SendAppRequestMsg{
		NodeIds:   nodeIDBytes,
		RequestId: requestID,
		Request:   request,
	})
	return err
}

func (c *Client) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	_, err := c.client.SendAppResponse(ctx, &appsenderpb.SendAppResponseMsg{
		NodeId:    nodeID[:],
		RequestId: requestID,
		Response:  response,
	})
	return err
}

func (c *Client) SendAppGossip(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	// For gossip, we don't track specific nodes in the RPC implementation
	// Just send the gossip message
	_, err := c.client.SendAppGossip(ctx, &appsenderpb.SendAppGossipMsg{
		Msg: appGossipBytes,
	})
	return err
}

func (c *Client) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	_, err := c.client.SendAppError(ctx, &appsenderpb.SendAppErrorMsg{
		NodeId:       nodeID[:],
		RequestId:    requestID,
		ErrorCode:    errorCode,
		ErrorMessage: errorMessage,
	})
	return err
}

func (c *Client) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	// Same as SendAppGossip for RPC implementation
	return c.SendAppGossip(ctx, nodeIDs, appGossipBytes)
}