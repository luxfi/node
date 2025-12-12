// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2ptest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/luxfi/metric"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/network/p2p"
)

func NewSelfClient(t *testing.T, ctx context.Context, nodeID ids.NodeID, handler p2p.Handler) *p2p.Client {
	return NewClient(t, ctx, nodeID, handler, nodeID, handler)
}

// NewClient generates a client-server pair and returns the client used to
// communicate with a server with the specified handler
func NewClient(
	t *testing.T,
	ctx context.Context,
	clientNodeID ids.NodeID,
	clientHandler p2p.Handler,
	serverNodeID ids.NodeID,
	serverHandler p2p.Handler,
) *p2p.Client {
	return NewClientWithPeers(
		t,
		ctx,
		clientNodeID,
		clientHandler,
		map[ids.NodeID]p2p.Handler{
			serverNodeID: serverHandler,
		},
	)
}

// NewClientWithPeers generates a client to communicate to a set of peers
func NewClientWithPeers(
	t *testing.T,
	ctx context.Context,
	clientNodeID ids.NodeID,
	clientHandler p2p.Handler,
	peers map[ids.NodeID]p2p.Handler,
) *p2p.Client {
	peers[clientNodeID] = clientHandler

	peerSenders := make(map[ids.NodeID]*AppSender)
	peerNetworks := make(map[ids.NodeID]*p2p.Network)
	for nodeID := range peers {
		peerSenders[nodeID] = &AppSender{T: t}
		peerNetwork, err := p2p.NewNetwork(log.NewNoOpLogger(), peerSenders[nodeID], metric.NewRegistry(), "")
		require.NoError(t, err)
		peerNetworks[nodeID] = peerNetwork
	}

	peerSenders[clientNodeID].SendGossipF = func(ctx context.Context, config p2p.SendConfig, gossipBytes []byte) error {
		// Send the request asynchronously to avoid deadlock when the server
		// sends the response back to the client
		for nodeID := range config.NodeIDs {
			// Send directly to the handler if it's the client node
			if nodeID == clientNodeID {
				go func() {
					_ = peerNetworks[clientNodeID].Gossip(ctx, clientNodeID, gossipBytes)
				}()
			} else {
				go func(nid ids.NodeID) {
					_ = peerNetworks[nid].Gossip(ctx, clientNodeID, gossipBytes)
				}(nodeID)
			}
		}

		return nil
	}

	peerSenders[clientNodeID].SendRequestF = func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error {
		for nodeID := range nodeIDs {
			network, ok := peerNetworks[nodeID]
			if !ok {
				return fmt.Errorf("%s is not connected", nodeID)
			}

			// Send the request asynchronously to avoid deadlock when the server
			// sends the response back to the client
			go func() {
				_, _ = network.Request(ctx, clientNodeID, requestID, time.Time{}, requestBytes)
			}()
		}

		return nil
	}

	for nodeID := range peers {
		peerSenders[nodeID].SendResponseF = func(ctx context.Context, _ ids.NodeID, requestID uint32, responseBytes []byte) error {
			// Send the request asynchronously to avoid deadlock when the server
			// sends the response back to the client
			go func() {
				_ = peerNetworks[clientNodeID].Response(ctx, nodeID, requestID, responseBytes)
			}()

			return nil
		}
	}

	for nodeID := range peers {
		peerSenders[nodeID].SendErrorF = func(ctx context.Context, _ ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
			go func() {
				_ = peerNetworks[clientNodeID].RequestFailed(ctx, nodeID, requestID, &p2p.Error{
					Code:    errorCode,
					Message: errorMessage,
				})
			}()

			return nil
		}
	}

	for nodeID := range peers {
		require.NoError(t, peerNetworks[nodeID].Connected(ctx, clientNodeID, nil))
		require.NoError(t, peerNetworks[nodeID].Connected(ctx, nodeID, nil))
		require.NoError(t, peerNetworks[nodeID].AddHandler(0, peers[nodeID]))
	}

	return peerNetworks[clientNodeID].NewClient(0)
}
