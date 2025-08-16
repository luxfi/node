package appsender

import (
	"context"

	"github.com/luxfi/consensus/core"
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

func (c *Client) SendAppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, request []byte) error {
	_, err := c.client.SendAppRequest(ctx, &appsenderpb.SendAppRequestMsg{
		NodeIds:   [][]byte{nodeID[:]},
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

func (c *Client) SendAppGossip(ctx context.Context, appGossipBytes []byte) error {
	_, err := c.client.SendAppGossip(ctx, &appsenderpb.SendAppGossipMsg{
		Msg: appGossipBytes,
	})
	return err
}

func (c *Client) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	// AppSender doesn't have SendAppError method, ignore
	return nil
}

func (c *Client) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	_, err := c.client.SendCrossChainAppRequest(ctx, &appsenderpb.SendCrossChainAppRequestMsg{
		ChainId:   chainID[:],
		RequestId: requestID,
		Request:   appRequestBytes,
	})
	return err
}

func (c *Client) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error {
	_, err := c.client.SendCrossChainAppResponse(ctx, &appsenderpb.SendCrossChainAppResponseMsg{
		ChainId:   chainID[:],
		RequestId: requestID,
		Response:  appResponseBytes,
	})
	return err
}
