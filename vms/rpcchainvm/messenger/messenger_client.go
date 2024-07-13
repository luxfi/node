// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package messenger

import (
	"context"

	"github.com/luxfi/node/snow/engine/common"

	messengerpb "github.com/luxfi/node/proto/pb/messenger"
)

// Client is an implementation of a messenger channel that talks over RPC.
type Client struct {
	client messengerpb.MessengerClient
}

// NewClient returns a client that is connected to a remote channel
func NewClient(client messengerpb.MessengerClient) *Client {
	return &Client{client: client}
}

func (c *Client) Notify(msg common.Message) error {
	_, err := c.client.Notify(context.Background(), &messengerpb.NotifyRequest{
		Message: messengerpb.Message(msg),
	})
	return err
}
