// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2ptest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/p2p"
	consensuscore "github.com/luxfi/consensus/core"
	"github.com/luxfi/math/set"
)

func TestClient_AppGossip(t *testing.T) {
	require := require.New(t)

	// Use context with timeout to prevent test hanging
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	appGossipChan := make(chan struct{})
	testHandler := p2p.TestHandler{
		AppGossipF: func(context.Context, ids.NodeID, []byte) {
			close(appGossipChan)
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
	require.NoError(client.AppGossip(ctx, consensuscore.SendConfig{
		NodeIDs: []interface{}{nodeID},
		Peers:   1,
	}, []byte("foobar")))
	
	// Wait for gossip with select to respect context
	select {
	case <-appGossipChan:
		// Success
	case <-ctx.Done():
		t.Fatal("test timed out waiting for AppGossip")
	}
}

func TestClient_AppRequest(t *testing.T) {
	tests := []struct {
		name        string
		appResponse []byte
		appErr      error
		appRequestF func(ctx context.Context, client *p2p.Client, onResponse p2p.AppResponseCallback) error
	}{
		{
			name:        "AppRequest - response",
			appResponse: []byte("foobar"),
			appRequestF: func(ctx context.Context, client *p2p.Client, onResponse p2p.AppResponseCallback) error {
				return client.AppRequest(ctx, set.Of(ids.EmptyNodeID), []byte("foo"), onResponse)
			},
		},
		{
			name: "AppRequest - error",
			appErr: &consensuscore.AppError{
				Code:    123,
				Message: "foobar",
			},
			appRequestF: func(ctx context.Context, client *p2p.Client, onResponse p2p.AppResponseCallback) error {
				return client.AppRequest(ctx, set.Of(ids.EmptyNodeID), []byte("foo"), onResponse)
			},
		},
		{
			name:        "AppRequestAny - response",
			appResponse: []byte("foobar"),
			appRequestF: func(ctx context.Context, client *p2p.Client, onResponse p2p.AppResponseCallback) error {
				return client.AppRequestAny(ctx, []byte("foo"), onResponse)
			},
		},
		{
			name: "AppRequestAny - error",
			appErr: &consensuscore.AppError{
				Code:    123,
				Message: "foobar",
			},
			appRequestF: func(ctx context.Context, client *p2p.Client, onResponse p2p.AppResponseCallback) error {
				return client.AppRequestAny(ctx, []byte("foo"), onResponse)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctx := context.Background()

			appRequestChan := make(chan struct{})
			testHandler := p2p.TestHandler{
				AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *consensuscore.AppError) {
					if tt.appErr != nil {
					return nil, tt.appErr.(*consensuscore.AppError)
					}

					return tt.appResponse, nil
				},
			}

			client := NewSelfClient(
				t,
				ctx,
				ids.EmptyNodeID,
				testHandler,
			)
			require.NoError(tt.appRequestF(
				ctx,
				client,
				func(_ context.Context, _ ids.NodeID, responseBytes []byte, err error) {
					defer close(appRequestChan)
					if tt.appErr != nil {
						require.Error(err)
						// Compare error properties since AppError doesn't implement Is()
						appErr, ok := err.(*consensuscore.AppError)
						require.True(ok, "error should be an AppError")
						expectedErr := tt.appErr.(*consensuscore.AppError)
						require.Equal(expectedErr.Code, appErr.Code)
						require.Equal(expectedErr.Message, appErr.Message)
					} else {
						require.NoError(err)
					}
					require.Equal(tt.appResponse, responseBytes)
				},
			))
			<-appRequestChan
		})
	}
}
