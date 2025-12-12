// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2ptest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/network/p2p"
)

func TestClient_Gossip(t *testing.T) {
	require := require.New(t)

	// Use context with timeout to prevent test hanging
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	gossipChan := make(chan struct{})
	testHandler := p2p.TestHandler{
		GossipF: func(context.Context, ids.NodeID, []byte) {
			close(gossipChan)
		},
	}

	nodeID := ids.GenerateTestNodeID()
	client := NewSelfClient(
		t,
		ctx,
		nodeID,
		testHandler,
	)
	// Explicitly specify the node to gossip to
	require.NoError(client.Gossip(ctx, p2p.SendConfig{
		NodeIDs: set.Of(nodeID),
		Peers:   1,
	}, []byte("foobar")))

	// Wait for gossip with select to respect context
	select {
	case <-gossipChan:
		// Success
	case <-ctx.Done():
		t.Fatal("test timed out waiting for Gossip")
	}
}

func TestClient_Request(t *testing.T) {
	tests := []struct {
		name        string
		response    []byte
		respErr     error
		requestF    func(ctx context.Context, client *p2p.Client, onResponse p2p.ResponseCallback) error
	}{
		{
			name:     "Request - response",
			response: []byte("foobar"),
			requestF: func(ctx context.Context, client *p2p.Client, onResponse p2p.ResponseCallback) error {
				return client.Request(ctx, set.Of(ids.EmptyNodeID), []byte("foo"), onResponse)
			},
		},
		{
			name: "Request - error",
			respErr: &p2p.Error{
				Code:    123,
				Message: "foobar",
			},
			requestF: func(ctx context.Context, client *p2p.Client, onResponse p2p.ResponseCallback) error {
				return client.Request(ctx, set.Of(ids.EmptyNodeID), []byte("foo"), onResponse)
			},
		},
		{
			name:     "RequestAny - response",
			response: []byte("foobar"),
			requestF: func(ctx context.Context, client *p2p.Client, onResponse p2p.ResponseCallback) error {
				return client.RequestAny(ctx, []byte("foo"), onResponse)
			},
		},
		{
			name: "RequestAny - error",
			respErr: &p2p.Error{
				Code:    123,
				Message: "foobar",
			},
			requestF: func(ctx context.Context, client *p2p.Client, onResponse p2p.ResponseCallback) error {
				return client.RequestAny(ctx, []byte("foo"), onResponse)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctx := context.Background()

			requestChan := make(chan struct{})
			testHandler := p2p.TestHandler{
				RequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *p2p.Error) {
					if tt.respErr != nil {
						return nil, tt.respErr.(*p2p.Error)
					}

					return tt.response, nil
				},
			}

			client := NewSelfClient(
				t,
				ctx,
				ids.EmptyNodeID,
				testHandler,
			)
			require.NoError(tt.requestF(
				ctx,
				client,
				func(_ context.Context, _ ids.NodeID, responseBytes []byte, err error) {
					defer close(requestChan)
					if tt.respErr != nil {
						require.Error(err)
						// Compare error properties since Error doesn't implement Is()
						respErr, ok := err.(*p2p.Error)
						require.True(ok, "error should be an Error")
						expectedErr := tt.respErr.(*p2p.Error)
						require.Equal(expectedErr.Code, respErr.Code)
						require.Equal(expectedErr.Message, respErr.Message)
					} else {
						require.NoError(err)
					}
					require.Equal(tt.response, responseBytes)
				},
			))
			<-requestChan
		})
	}
}
