//go:build grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package rpckeystore is the keystore-over-RPC client and server. The
// transport is selected by build tag: `grpc` selects the legacy gRPC
// codepath in this file; the default build (no tag) will eventually
// select a ZAP-based codepath (task #57). Mirrors the rpcdb naming
// pattern — `rpc` is the value (remote procedure call), the substrate
// is per-tag.
package rpckeystore

import (
	"context"

	"github.com/luxfi/database"
	"github.com/luxfi/database/encdb"
	"github.com/luxfi/node/service/keystore"
	"github.com/luxfi/node/internal/database/rpcdb"
	"github.com/luxfi/vm/rpc/grpcutils"

	keystorepb "github.com/luxfi/node/proto/pb/keystore"
	rpcdbpb "github.com/luxfi/node/proto/pb/rpcdb"
)

var _ keystore.BlockchainKeystore = (*Client)(nil)

// Client is a consensus.Keystore that talks over RPC.
type Client struct {
	client keystorepb.KeystoreClient
}

// NewClient returns a keystore instance connected to a remote keystore instance
func NewClient(client keystorepb.KeystoreClient) *Client {
	return &Client{
		client: client,
	}
}

func (c *Client) GetDatabase(username, password string) (*encdb.Database, error) {
	bcDB, err := c.GetRawDatabase(username, password)
	if err != nil {
		return nil, err
	}
	return encdb.New([]byte(password), bcDB)
}

func (c *Client) GetRawDatabase(username, password string) (database.Database, error) {
	resp, err := c.client.GetDatabase(context.Background(), &keystorepb.GetDatabaseRequest{
		Username: username,
		Password: password,
	})
	if err != nil {
		return nil, err
	}

	clientConn, err := grpcutils.Dial(resp.ServerAddr)
	if err != nil {
		return nil, err
	}

	dbClient := rpcdb.NewClient(rpcdbpb.NewDatabaseClient(clientConn))
	return dbClient, nil
}
