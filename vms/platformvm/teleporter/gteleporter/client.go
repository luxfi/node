// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package gteleporter

import (
	"context"

	"github.com/luxdefi/node/vms/platformvm/teleporter"

	pb "github.com/luxdefi/node/proto/pb/teleporter"
)

var _ teleporter.Signer = (*Client)(nil)

type Client struct {
	client pb.SignerClient
}

func NewClient(client pb.SignerClient) *Client {
	return &Client{client: client}
}

func (c *Client) Sign(unsignedMsg *teleporter.UnsignedMessage) ([]byte, error) {
	resp, err := c.client.Sign(context.Background(), &pb.SignRequest{
		SourceChainId:      unsignedMsg.SourceChainID[:],
		DestinationChainId: unsignedMsg.DestinationChainID[:],
		Payload:            unsignedMsg.Payload,
	})
	if err != nil {
		return nil, err
	}
	return resp.Signature, nil
}
