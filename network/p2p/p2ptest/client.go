// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2ptest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/core"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/network/p2p"
	consensusset "github.com/luxfi/consensus/utils/set"
)

// testAppSender implements core.AppSender for testing
type testAppSender struct {
	sendAppGossipF             func(context.Context, consensusset.Set[ids.NodeID], []byte) error
	sendAppRequestF            func(context.Context, consensusset.Set[ids.NodeID], uint32, []byte) error
	sendAppResponseF           func(context.Context, ids.NodeID, uint32, []byte) error
	sendAppErrorF              func(context.Context, ids.NodeID, uint32, int32, string) error
	sendAppGossipSpecificF     func(context.Context, consensusset.Set[ids.NodeID], []byte) error
}

func (t *testAppSender) SendAppGossip(ctx context.Context, nodeIDs consensusset.Set[ids.NodeID], appGossipBytes []byte) error {
	if t.sendAppGossipF != nil {
		return t.sendAppGossipF(ctx, nodeIDs, appGossipBytes)
	}
	return nil
}

func (t *testAppSender) SendAppRequest(ctx context.Context, nodeIDs consensusset.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error {
	if t.sendAppRequestF != nil {
		return t.sendAppRequestF(ctx, nodeIDs, requestID, appRequestBytes)
	}
	return nil
}

func (t *testAppSender) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	if t.sendAppResponseF != nil {
		return t.sendAppResponseF(ctx, nodeID, requestID, appResponseBytes)
	}
	return nil
}

func (t *testAppSender) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	if t.sendAppErrorF != nil {
		return t.sendAppErrorF(ctx, nodeID, requestID, errorCode, errorMessage)
	}
	return nil
}

func (t *testAppSender) SendAppGossipSpecific(ctx context.Context, nodeIDs consensusset.Set[ids.NodeID], appGossipBytes []byte) error {
	if t.sendAppGossipSpecificF != nil {
		return t.sendAppGossipSpecificF(ctx, nodeIDs, appGossipBytes)
	}
	return nil
}

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

	peerSenders := make(map[ids.NodeID]*testAppSender)
	peerNetworks := make(map[ids.NodeID]*p2p.Network)
	for nodeID := range peers {
		peerSenders[nodeID] = &testAppSender{}
		peerNetwork, err := p2p.NewNetwork(log.NewNoOpLogger(), peerSenders[nodeID], prometheus.NewRegistry(), "")
		require.NoError(t, err)
		peerNetworks[nodeID] = peerNetwork
	}

	peerSenders[clientNodeID].sendAppGossipF = func(ctx context.Context, nodeIDs consensusset.Set[ids.NodeID], gossipBytes []byte) error {
		// Send the gossip to all connected peers asynchronously to avoid deadlock
		// when the server sends the response back to the client
		for nodeID, network := range peerNetworks {
			if nodeID != clientNodeID {
				go func(nodeID ids.NodeID, network *p2p.Network) {
					_ = network.AppGossip(ctx, nodeID, gossipBytes)
				}(nodeID, network)
			}
		}

		return nil
	}

	peerSenders[clientNodeID].sendAppRequestF = func(ctx context.Context, nodeIDs consensusset.Set[ids.NodeID], requestID uint32, requestBytes []byte) error {
		// Send to the first node in the set
		for nodeID := range nodeIDs {
			network, ok := peerNetworks[nodeID]
			if !ok {
				return fmt.Errorf("%s is not connected", nodeID)
			}

			// Send the request asynchronously to avoid deadlock when the server
			// sends the response back to the client
			go func() {
				_ = network.AppRequest(ctx, clientNodeID, requestID, time.Time{}, requestBytes)
			}()
			
			break // Only send to first node
		}

		return nil
	}

	for nodeID := range peers {
		peerSenders[nodeID].sendAppResponseF = func(ctx context.Context, _ ids.NodeID, requestID uint32, responseBytes []byte) error {
			// Send the request asynchronously to avoid deadlock when the server
			// sends the response back to the client
			go func() {
				_ = peerNetworks[clientNodeID].AppResponse(ctx, nodeID, requestID, responseBytes)
			}()

			return nil
		}
	}

	for nodeID := range peers {
		peerSenders[nodeID].sendAppErrorF = func(ctx context.Context, _ ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
			go func() {
				_ = peerNetworks[clientNodeID].AppRequestFailed(ctx, nodeID, requestID, &core.AppError{
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
