// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	validators "github.com/luxfi/consensus/validator"
	validatorstest "github.com/luxfi/consensus/validator/validatorstest"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/metric"
	"github.com/luxfi/warp"
)

const (
	handlerID     = 123
	handlerPrefix = byte(handlerID)
)

var errFoo = &warp.Error{
	Code:    123,
	Message: "foo",
}

// testSender is a test implementation of warp.Sender for this package's tests.
type testSender struct {
	t             *testing.T
	SendRequestF  func(context.Context, set.Set[ids.NodeID], uint32, []byte) error
	SendResponseF func(context.Context, ids.NodeID, uint32, []byte) error
	SendErrorF    func(context.Context, ids.NodeID, uint32, int32, string) error
	SendGossipF   func(context.Context, warp.SendConfig, []byte) error
}

var _ warp.Sender = (*testSender)(nil)

func (s *testSender) SendRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, msg []byte) error {
	if s.SendRequestF != nil {
		return s.SendRequestF(ctx, nodeIDs, requestID, msg)
	}
	return nil
}

func (s *testSender) SendResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	if s.SendResponseF != nil {
		return s.SendResponseF(ctx, nodeID, requestID, msg)
	}
	return nil
}

func (s *testSender) SendError(ctx context.Context, nodeID ids.NodeID, requestID uint32, code int32, message string) error {
	if s.SendErrorF != nil {
		return s.SendErrorF(ctx, nodeID, requestID, code, message)
	}
	return nil
}

func (s *testSender) SendGossip(ctx context.Context, config warp.SendConfig, msg []byte) error {
	if s.SendGossipF != nil {
		return s.SendGossipF(ctx, config, msg)
	}
	return nil
}

func TestMessageRouting(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	wantNodeID := ids.GenerateTestNodeID()
	wantMsg := []byte("message")

	var gossipCalled, requestCalled bool
	testHandler := &TestHandler{
		GossipF: func(_ context.Context, nodeID ids.NodeID, msg []byte) {
			gossipCalled = true
			require.Equal(wantNodeID, nodeID)
			require.Equal(wantMsg, msg)
		},
		RequestF: func(_ context.Context, nodeID ids.NodeID, _ time.Time, msg []byte) ([]byte, *warp.Error) {
			requestCalled = true
			require.Equal(wantNodeID, nodeID)
			require.Equal(wantMsg, msg)
			return nil, nil
		},
	}

	sentGossip := make(chan []byte, 1)
	sentRequest := make(chan []byte, 1)
	sender := &testSender{
		t: t,
		SendGossipF: func(_ context.Context, _ warp.SendConfig, msg []byte) error {
			sentGossip <- msg
			return nil
		},
		SendRequestF: func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, msg []byte) error {
			sentRequest <- msg
			return nil
		},
	}

	network, err := NewNetwork(log.NewNoOpLogger(), sender, metric.NewRegistry(), "")
	require.NoError(err)
	require.NoError(network.AddHandler(1, testHandler))
	client := network.NewClient(1)

	require.NoError(client.Gossip(
		ctx,
		warp.SendConfig{
			Peers: 1,
		},
		wantMsg,
	))
	gossipBytes := <-sentGossip
	t.Logf("Sent Gossip bytes: %x", gossipBytes)
	err = network.Gossip(ctx, wantNodeID, gossipBytes)
	if err != nil {
		t.Logf("Gossip error: %v", err)
	}
	require.NoError(err)
	require.True(gossipCalled)

	require.NoError(client.Request(ctx, set.Of(ids.EmptyNodeID), wantMsg, func(context.Context, ids.NodeID, []byte, error) {}))
	requestBytes := <-sentRequest
	t.Logf("Sent Request bytes: %x", requestBytes)
	_, requestErr := network.Request(ctx, wantNodeID, 1, time.Time{}, requestBytes)
	if requestErr != nil {
		t.Logf("Request error: %v", requestErr)
	}
	require.Nil(requestErr)
	require.True(requestCalled)
}

// Tests that the Client prefixes messages with the handler prefix
func TestClientPrefixesMessages(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sentRequest := make(chan []byte, 1)
	sentGossip := make(chan []byte, 1)
	sender := &testSender{
		t: t,
		SendRequestF: func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, msg []byte) error {
			sentRequest <- msg
			return nil
		},
		SendGossipF: func(_ context.Context, _ warp.SendConfig, msg []byte) error {
			sentGossip <- msg
			return nil
		},
	}

	network, err := NewNetwork(log.NewNoOpLogger(), sender, metric.NewRegistry(), "")
	require.NoError(err)
	require.NoError(network.Connected(ctx, ids.EmptyNodeID, nil))
	client := network.NewClient(handlerID)

	want := []byte("message")

	require.NoError(client.Request(
		ctx,
		set.Of(ids.EmptyNodeID),
		want,
		func(context.Context, ids.NodeID, []byte, error) {},
	))
	gotRequest := <-sentRequest
	require.Equal(handlerPrefix, gotRequest[0])
	require.Equal(want, gotRequest[1:])

	require.NoError(client.RequestAny(
		ctx,
		want,
		func(context.Context, ids.NodeID, []byte, error) {},
	))
	gotRequest = <-sentRequest
	require.Equal(handlerPrefix, gotRequest[0])
	require.Equal(want, gotRequest[1:])

	require.NoError(client.Gossip(
		ctx,
		warp.SendConfig{
			Peers: 1,
		},
		want,
	))
	gotGossip := <-sentGossip
	require.Equal(handlerPrefix, gotGossip[0])
	require.Equal(want, gotGossip[1:])
}

// Tests that the Client callback is called on a successful response
func TestRequestResponse(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sentRequest := make(chan []byte, 1)
	sender := &testSender{
		t: t,
		SendRequestF: func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, msg []byte) error {
			sentRequest <- msg
			return nil
		},
	}
	network, err := NewNetwork(log.NewNoOpLogger(), sender, metric.NewRegistry(), "")
	require.NoError(err)
	client := network.NewClient(handlerID)

	wantResponse := []byte("response")
	wantNodeID := ids.GenerateTestNodeID()
	done := make(chan struct{})

	callback := func(_ context.Context, gotNodeID ids.NodeID, gotResponse []byte, err error) {
		require.Equal(wantNodeID, gotNodeID)
		require.NoError(err)
		require.Equal(wantResponse, gotResponse)

		close(done)
	}

	want := []byte("request")
	require.NoError(client.Request(ctx, set.Of(wantNodeID), want, callback))
	got := <-sentRequest
	require.Equal(handlerPrefix, got[0])
	require.Equal(want, got[1:])

	require.NoError(network.Response(ctx, wantNodeID, 1, wantResponse))
	<-done
}

// Tests that the Client does not provide a cancelled context to the Sender.
func TestRequestCancelledContext(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sentMessages := make(chan []byte, 1)
	sender := &testSender{
		t: t,
		SendRequestF: func(ctx context.Context, _ set.Set[ids.NodeID], _ uint32, msgBytes []byte) error {
			require.NoError(ctx.Err())
			sentMessages <- msgBytes
			return nil
		},
	}
	network, err := NewNetwork(log.NewNoOpLogger(), sender, metric.NewRegistry(), "")
	require.NoError(err)
	client := network.NewClient(handlerID)

	wantResponse := []byte("response")
	wantNodeID := ids.GenerateTestNodeID()
	done := make(chan struct{})

	callback := func(_ context.Context, gotNodeID ids.NodeID, gotResponse []byte, err error) {
		require.Equal(wantNodeID, gotNodeID)
		require.NoError(err)
		require.Equal(wantResponse, gotResponse)

		close(done)
	}

	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()

	want := []byte("request")
	require.NoError(client.Request(cancelledCtx, set.Of(wantNodeID), want, callback))
	got := <-sentMessages
	require.Equal(handlerPrefix, got[0])
	require.Equal(want, got[1:])

	require.NoError(network.Response(ctx, wantNodeID, 1, wantResponse))
	<-done
}

// Tests that the Client callback is given an error if the request fails
func TestRequestFailed(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sentRequest := make(chan []byte, 1)
	sender := &testSender{
		t: t,
		SendRequestF: func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, msg []byte) error {
			sentRequest <- msg
			return nil
		},
	}
	network, err := NewNetwork(log.NewNoOpLogger(), sender, metric.NewRegistry(), "")
	require.NoError(err)
	client := network.NewClient(handlerID)

	wantNodeID := ids.GenerateTestNodeID()
	done := make(chan struct{})

	callback := func(_ context.Context, gotNodeID ids.NodeID, gotResponse []byte, err error) {
		require.Equal(wantNodeID, gotNodeID)
		require.ErrorIs(err, errFoo)
		require.Nil(gotResponse)

		close(done)
	}

	require.NoError(client.Request(ctx, set.Of(wantNodeID), []byte("request"), callback))
	<-sentRequest

	require.NoError(network.RequestFailed(ctx, wantNodeID, 1, errFoo))
	<-done
}

// Messages for unregistered handlers should be dropped gracefully
func TestGossipMessageForUnregisteredHandler(t *testing.T) {
	tests := []struct {
		name string
		msg  []byte
	}{
		{
			name: "nil",
			msg:  nil,
		},
		{
			name: "empty",
			msg:  []byte{},
		},
		{
			name: "non-empty",
			msg:  []byte("foobar"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctx := context.Background()
			handler := &TestHandler{
				GossipF: func(context.Context, ids.NodeID, []byte) {
					require.Fail("should not be called")
				},
			}
			network, err := NewNetwork(log.NewNoOpLogger(), nil, metric.NewRegistry(), "")
			require.NoError(err)
			require.NoError(network.AddHandler(handlerID, handler))
			require.NoError(network.Gossip(ctx, ids.EmptyNodeID, tt.msg))
		})
	}
}

// An unregistered handler should gracefully drop messages by responding
// to the requester with a warp.Error
func TestRequestMessageForUnregisteredHandler(t *testing.T) {
	tests := []struct {
		name string
		msg  []byte
	}{
		{
			name: "nil",
			msg:  nil,
		},
		{
			name: "empty",
			msg:  []byte{},
		},
		{
			name: "non-empty",
			msg:  []byte("foobar"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctx := context.Background()
			handler := &TestHandler{
				RequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *warp.Error) {
					require.Fail("should not be called")
					return nil, nil
				},
			}

			wantNodeID := ids.GenerateTestNodeID()
			wantRequestID := uint32(111)

			sender := &testSender{t: t}
			network, err := NewNetwork(log.NewNoOpLogger(), sender, metric.NewRegistry(), "")
			require.NoError(err)
			require.NoError(network.AddHandler(handlerID, handler))

			// Request with unregistered handler message should return ErrUnregisteredHandler
			_, reqErr := network.Request(ctx, wantNodeID, wantRequestID, time.Time{}, tt.msg)
			require.Equal(ErrUnregisteredHandler, reqErr)
		})
	}
}

// A handler that errors should send an Error to the requesting peer
func TestHandlerError(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	handlerError := &warp.Error{
		Code:    123,
		Message: "foo",
	}
	handler := &TestHandler{
		RequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *warp.Error) {
			return nil, handlerError
		},
	}

	wantNodeID := ids.GenerateTestNodeID()
	wantRequestID := uint32(111)

	done := make(chan struct{})
	sender := &testSender{
		t: t,
		SendErrorF: func(_ context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
			defer close(done)

			require.Equal(wantNodeID, nodeID)
			require.Equal(wantRequestID, requestID)
			require.Equal(handlerError.Code, errorCode)
			require.Equal(handlerError.Message, errorMessage)

			return nil
		},
	}
	network, err := NewNetwork(log.NewNoOpLogger(), sender, metric.NewRegistry(), "")
	require.NoError(err)
	require.NoError(network.AddHandler(handlerID, handler))
	msg := PrefixMessage(ProtocolPrefix(handlerID), []byte("message"))

	_, reqErr := network.Request(ctx, wantNodeID, wantRequestID, time.Time{}, msg)
	require.Nil(reqErr)
	<-done
}

// A response or timeout for a request we never made should return an error
func TestResponseForUnrequestedRequest(t *testing.T) {
	tests := []struct {
		name string
		msg  []byte
	}{
		{
			name: "nil",
			msg:  nil,
		},
		{
			name: "empty",
			msg:  []byte{},
		},
		{
			name: "non-empty",
			msg:  []byte("foobar"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctx := context.Background()
			handler := &TestHandler{
				GossipF: func(context.Context, ids.NodeID, []byte) {
					require.Fail("should not be called")
				},
				RequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *warp.Error) {
					require.Fail("should not be called")
					return nil, nil
				},
			}
			network, err := NewNetwork(log.NewNoOpLogger(), nil, metric.NewRegistry(), "")
			require.NoError(err)
			require.NoError(network.AddHandler(handlerID, handler))

			err = network.Response(ctx, ids.EmptyNodeID, 0, []byte("foobar"))
			require.ErrorIs(err, ErrUnrequestedResponse)
			err = network.RequestFailed(ctx, ids.EmptyNodeID, 0, &warp.Error{Code: -1, Message: "timeout"})
			require.ErrorIs(err, ErrUnrequestedResponse)
		})
	}
}

// It's possible for the request id to overflow and wrap around.
// If there are still pending requests with the same request id, we should
// not attempt to issue another request until the previous one has cleared.
func TestRequestDuplicateRequestIDs(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sentRequest := make(chan []byte, 1)
	sender := &testSender{
		t: t,
		SendRequestF: func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, msg []byte) error {
			sentRequest <- msg
			return nil
		},
	}

	network, err := NewNetwork(log.NewNoOpLogger(), sender, metric.NewRegistry(), "")
	require.NoError(err)
	client := network.NewClient(0x1)

	noOpCallback := func(context.Context, ids.NodeID, []byte, error) {}
	// create a request that never gets a response
	network.router.requestID = 1
	require.NoError(client.Request(ctx, set.Of(ids.EmptyNodeID), []byte{}, noOpCallback))
	<-sentRequest

	// force the network to use the same requestID
	network.router.requestID = 1
	err = client.Request(context.Background(), set.Of(ids.EmptyNodeID), []byte{}, noOpCallback)
	require.ErrorIs(err, ErrRequestPending)
}

// Sample should always return up to [limit] peers, and less if fewer than
// [limit] peers are available.
func TestPeersSample(t *testing.T) {
	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()
	nodeID3 := ids.GenerateTestNodeID()

	tests := []struct {
		name         string
		connected    set.Set[ids.NodeID]
		disconnected set.Set[ids.NodeID]
		limit        int
	}{
		{
			name:  "no peers",
			limit: 1,
		},
		{
			name:      "one peer connected",
			connected: set.Of(nodeID1),
			limit:     1,
		},
		{
			name:      "multiple peers connected",
			connected: set.Of(nodeID1, nodeID2, nodeID3),
			limit:     1,
		},
		{
			name:         "peer connects and disconnects - 1",
			connected:    set.Of(nodeID1),
			disconnected: set.Of(nodeID1),
			limit:        1,
		},
		{
			name:         "peer connects and disconnects - 2",
			connected:    set.Of(nodeID1, nodeID2),
			disconnected: set.Of(nodeID2),
			limit:        1,
		},
		{
			name:         "peer connects and disconnects - 2",
			connected:    set.Of(nodeID1, nodeID2, nodeID3),
			disconnected: set.Of(nodeID1, nodeID2),
			limit:        1,
		},
		{
			name:      "less than limit peers",
			connected: set.Of(nodeID1, nodeID2, nodeID3),
			limit:     4,
		},
		{
			name:      "limit peers",
			connected: set.Of(nodeID1, nodeID2, nodeID3),
			limit:     3,
		},
		{
			name:      "more than limit peers",
			connected: set.Of(nodeID1, nodeID2, nodeID3),
			limit:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			network, err := NewNetwork(log.NewNoOpLogger(), &testSender{t: t}, metric.NewRegistry(), "")
			require.NoError(err)

			// Connect peers
			for nodeID := range tt.connected {
				t.Logf("Connecting %s", nodeID)
				require.NoError(network.Connected(context.Background(), nodeID, nil))
			}

			// Disconnect peers
			for nodeID := range tt.disconnected {
				t.Logf("Disconnecting %s", nodeID)
				require.NoError(network.Disconnected(context.Background(), nodeID))
			}

			// Calculate expected sampleable set: connected - disconnected
			sampleable := set.NewSet[ids.NodeID](0)
			sampleable = sampleable.Union(tt.connected)
			sampleable = sampleable.Difference(tt.disconnected)

			t.Logf("Connected: %v, Disconnected: %v, Sampleable: %v (len=%d)",
				tt.connected, tt.disconnected, sampleable, sampleable.Len())

			// Sample from the network
			sampled := network.Peers.Sample(tt.limit)

			// Expected sample size
			expectedLen := min(tt.limit, sampleable.Len())

			// If sampleable is empty, we should get no samples
			if sampleable.Len() == 0 {
				require.Empty(sampled, "expected no samples when no peers available, but got %v", sampled)
			} else {
				require.Len(sampled, expectedLen, "expected %d samples but got %d, sampleable: %v, sampled: %v",
					expectedLen, len(sampled), sampleable, sampled)

				// All sampled nodes should be in the sampleable set
				for _, nodeID := range sampled {
					require.True(sampleable.Contains(nodeID), "sampled node %s not in sampleable set %v", nodeID, sampleable)
				}
			}
		})
	}
}

func TestRequestAnyNodeSelection(t *testing.T) {
	tests := []struct {
		name     string
		peers    []ids.NodeID
		expected error
	}{
		{
			name:     "no peers",
			expected: ErrNoPeers,
		},
		{
			name:  "has peers",
			peers: []ids.NodeID{ids.GenerateTestNodeID()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			var sent ids.NodeID
			sender := &testSender{
				t: t,
				SendRequestF: func(_ context.Context, nodeIDs set.Set[ids.NodeID], _ uint32, _ []byte) error {
					nodeID := nodeIDs.List()[0]
					sent = nodeID
					return nil
				},
			}

			n, err := NewNetwork(log.NewNoOpLogger(), sender, metric.NewRegistry(), "")
			require.NoError(err)
			for _, peer := range tt.peers {
				require.NoError(n.Connected(context.Background(), peer, nil))
			}

			client := n.NewClient(1)

			err = client.RequestAny(context.Background(), []byte("foobar"), nil)
			require.ErrorIs(err, tt.expected)
			if len(tt.peers) > 0 && tt.expected == nil {
				require.Contains(tt.peers, sent)
			}
		})
	}
}

func TestNodeSamplerClientOption(t *testing.T) {
	nodeID0 := ids.GenerateTestNodeID()
	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()

	tests := []struct {
		name        string
		peers       []ids.NodeID
		option      func(t *testing.T, n *Network) ClientOption
		expected    []ids.NodeID
		expectedErr error
	}{
		{
			name:  "default",
			peers: []ids.NodeID{nodeID0, nodeID1, nodeID2},
			option: func(*testing.T, *Network) ClientOption {
				return clientOptionFunc(func(*clientOptions) {})
			},
			expected: []ids.NodeID{nodeID0, nodeID1, nodeID2},
		},
		{
			name:  "validator connected",
			peers: []ids.NodeID{nodeID0, nodeID1},
			option: func(_ *testing.T, n *Network) ClientOption {
				state := &validatorstest.State{
					GetCurrentHeightF: func(context.Context) (uint64, error) {
						return 0, nil
					},
					GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
						return map[ids.NodeID]*validators.GetValidatorOutput{
							nodeID1: {
								NodeID: nodeID1,
								Weight: 1,
							},
						}, nil
					},
				}

				validators := NewValidators(n.Peers, n.log, ids.Empty, state, 0)
				return WithValidatorSampling(validators)
			},
			expected: []ids.NodeID{nodeID1},
		},
		{
			name:  "validator disconnected",
			peers: []ids.NodeID{nodeID0},
			option: func(_ *testing.T, n *Network) ClientOption {
				state := &validatorstest.State{
					GetCurrentHeightF: func(context.Context) (uint64, error) {
						return 0, nil
					},
					GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
						return map[ids.NodeID]*validators.GetValidatorOutput{
							nodeID1: {
								NodeID: nodeID1,
								Weight: 1,
							},
						}, nil
					},
				}

				validators := NewValidators(n.Peers, n.log, ids.Empty, state, 0)
				return WithValidatorSampling(validators)
			},
			expectedErr: ErrNoPeers,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			done := make(chan struct{})
			sender := &testSender{
				t: t,
				SendRequestF: func(_ context.Context, nodeIDs set.Set[ids.NodeID], _ uint32, _ []byte) error {
					nodeID := nodeIDs.List()[0]
					if len(tt.expected) > 0 {
						require.Contains(tt.expected, nodeID)
					}

					close(done)
					return nil
				},
			}
			network, err := NewNetwork(log.NewNoOpLogger(), sender, metric.NewRegistry(), "")
			require.NoError(err)
			ctx := context.Background()
			for _, peer := range tt.peers {
				require.NoError(network.Connected(ctx, peer, nil))
			}

			client := network.NewClient(0, tt.option(t, network))

			if err = client.RequestAny(ctx, []byte("request"), nil); err != nil {
				close(done)
			}

			require.ErrorIs(err, tt.expectedErr)
			<-done
		})
	}
}

// Tests that a given protocol can have more than one client
func TestMultipleClients(t *testing.T) {
	require := require.New(t)

	n, err := NewNetwork(log.NewNoOpLogger(), &testSender{t: t}, metric.NewRegistry(), "")
	require.NoError(err)
	_ = n.NewClient(0)
	_ = n.NewClient(0)
}
