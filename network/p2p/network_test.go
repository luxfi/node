// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/luxfi/metric"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	consensuscore "github.com/luxfi/consensus/core"
	validators "github.com/luxfi/consensus/validator"
	validatorstest "github.com/luxfi/consensus/validator/validatorstest"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
)

const (
	handlerID     = 123
	handlerPrefix = byte(handlerID)
)

var errFoo = &consensuscore.AppError{
	Code:    123,
	Message: "foo",
}

// testSender is a test implementation of WarpSender for this package's tests.
type testSender struct {
	t                          *testing.T
	SendAppRequestF            func(context.Context, set.Set[ids.NodeID], uint32, []byte) error
	SendAppResponseF           func(context.Context, ids.NodeID, uint32, []byte) error
	SendAppErrorF              func(context.Context, ids.NodeID, uint32, int32, string) error
	SendAppGossipF             func(context.Context, set.Set[ids.NodeID], []byte) error
	SendCrossChainAppRequestF  func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppResponseF func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppErrorF    func(context.Context, ids.ID, uint32, int32, string) error
}

func (s *testSender) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, msg []byte) error {
	if s.SendAppRequestF != nil {
		return s.SendAppRequestF(ctx, nodeIDs, requestID, msg)
	}
	return nil
}

func (s *testSender) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	if s.SendAppResponseF != nil {
		return s.SendAppResponseF(ctx, nodeID, requestID, msg)
	}
	return nil
}

func (s *testSender) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, code int32, message string) error {
	if s.SendAppErrorF != nil {
		return s.SendAppErrorF(ctx, nodeID, requestID, code, message)
	}
	return nil
}

func (s *testSender) SendAppGossip(ctx context.Context, nodeIDs set.Set[ids.NodeID], msg []byte) error {
	if s.SendAppGossipF != nil {
		return s.SendAppGossipF(ctx, nodeIDs, msg)
	}
	return nil
}

func (s *testSender) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], msg []byte) error {
	return s.SendAppGossip(ctx, nodeIDs, msg)
}

func (s *testSender) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	if s.SendCrossChainAppRequestF != nil {
		return s.SendCrossChainAppRequestF(ctx, chainID, requestID, msg)
	}
	return nil
}

func (s *testSender) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	if s.SendCrossChainAppResponseF != nil {
		return s.SendCrossChainAppResponseF(ctx, chainID, requestID, msg)
	}
	return nil
}

func (s *testSender) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, code int32, message string) error {
	if s.SendCrossChainAppErrorF != nil {
		return s.SendCrossChainAppErrorF(ctx, chainID, requestID, code, message)
	}
	return nil
}

func TestMessageRouting(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	wantNodeID := ids.GenerateTestNodeID()
	wantMsg := []byte("message")

	var appGossipCalled, appRequestCalled bool
	testHandler := &TestHandler{
		AppGossipF: func(_ context.Context, nodeID ids.NodeID, msg []byte) {
			appGossipCalled = true
			require.Equal(wantNodeID, nodeID)
			require.Equal(wantMsg, msg)
		},
		AppRequestF: func(_ context.Context, nodeID ids.NodeID, _ time.Time, msg []byte) ([]byte, *consensuscore.AppError) {
			appRequestCalled = true
			require.Equal(wantNodeID, nodeID)
			require.Equal(wantMsg, msg)
			return nil, nil
		},
	}

	sentAppGossip := make(chan []byte, 1)
	sentAppRequest := make(chan []byte, 1)
	sender := &testSender{
		t: t,
		SendAppGossipF: func(_ context.Context, _ set.Set[ids.NodeID], msg []byte) error {
			sentAppGossip <- msg
			return nil
		},
		SendAppRequestF: func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, msg []byte) error {
			sentAppRequest <- msg
			return nil
		},
	}

	network, err := NewNetwork(log.NewNoOpLogger(), sender, metric.NewRegistry(), "")
	require.NoError(err)
	require.NoError(network.AddHandler(1, testHandler))
	client := network.NewClient(1)

	require.NoError(client.AppGossip(
		ctx,
		consensuscore.SendConfig{
			Peers: 1,
		},
		wantMsg,
	))
	gossipBytes := <-sentAppGossip
	t.Logf("Sent AppGossip bytes: %x", gossipBytes)
	err = network.AppGossip(ctx, wantNodeID, gossipBytes)
	if err != nil {
		t.Logf("AppGossip error: %v", err)
	}
	require.NoError(err)
	require.True(appGossipCalled)

	require.NoError(client.AppRequest(ctx, set.Of(ids.EmptyNodeID), wantMsg, func(context.Context, ids.NodeID, []byte, error) {}))
	requestBytes := <-sentAppRequest
	t.Logf("Sent AppRequest bytes: %x", requestBytes)
	err = network.AppRequest(ctx, wantNodeID, 1, time.Time{}, requestBytes)
	if err != nil {
		t.Logf("AppRequest error: %v", err)
	}
	require.NoError(err)
	require.True(appRequestCalled)
}

// Tests that the Client prefixes messages with the handler prefix
func TestClientPrefixesMessages(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sentAppRequest := make(chan []byte, 1)
	sentAppGossip := make(chan []byte, 1)
	sender := &testSender{
		t: t,
		SendAppRequestF: func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, msg []byte) error {
			sentAppRequest <- msg
			return nil
		},
		SendAppGossipF: func(_ context.Context, _ set.Set[ids.NodeID], msg []byte) error {
			sentAppGossip <- msg
			return nil
		},
	}

	network, err := NewNetwork(log.NewNoOpLogger(), sender, metric.NewRegistry(), "")
	require.NoError(err)
	require.NoError(network.Connected(ctx, ids.EmptyNodeID, nil))
	client := network.NewClient(handlerID)

	want := []byte("message")

	require.NoError(client.AppRequest(
		ctx,
		set.Of(ids.EmptyNodeID),
		want,
		func(context.Context, ids.NodeID, []byte, error) {},
	))
	gotAppRequest := <-sentAppRequest
	require.Equal(handlerPrefix, gotAppRequest[0])
	require.Equal(want, gotAppRequest[1:])

	require.NoError(client.AppRequestAny(
		ctx,
		want,
		func(context.Context, ids.NodeID, []byte, error) {},
	))
	gotAppRequest = <-sentAppRequest
	require.Equal(handlerPrefix, gotAppRequest[0])
	require.Equal(want, gotAppRequest[1:])

	require.NoError(client.AppGossip(
		ctx,
		consensuscore.SendConfig{
			Peers: 1,
		},
		want,
	))
	gotAppGossip := <-sentAppGossip
	require.Equal(handlerPrefix, gotAppGossip[0])
	require.Equal(want, gotAppGossip[1:])
}

// Tests that the Client callback is called on a successful response
func TestAppRequestResponse(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sentAppRequest := make(chan []byte, 1)
	sender := &testSender{
		t: t,
		SendAppRequestF: func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, msg []byte) error {
			sentAppRequest <- msg
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
	require.NoError(client.AppRequest(ctx, set.Of(wantNodeID), want, callback))
	got := <-sentAppRequest
	require.Equal(handlerPrefix, got[0])
	require.Equal(want, got[1:])

	require.NoError(network.AppResponse(ctx, wantNodeID, 1, wantResponse))
	<-done
}

// Tests that the Client does not provide a cancelled context to the AppSender.
func TestAppRequestCancelledContext(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sentMessages := make(chan []byte, 1)
	sender := &testSender{
		t: t,
		SendAppRequestF: func(ctx context.Context, _ set.Set[ids.NodeID], _ uint32, msgBytes []byte) error {
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
	require.NoError(client.AppRequest(cancelledCtx, set.Of(wantNodeID), want, callback))
	got := <-sentMessages
	require.Equal(handlerPrefix, got[0])
	require.Equal(want, got[1:])

	require.NoError(network.AppResponse(ctx, wantNodeID, 1, wantResponse))
	<-done
}

// Tests that the Client callback is given an error if the request fails
func TestAppRequestFailed(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sentAppRequest := make(chan []byte, 1)
	sender := &testSender{
		t: t,
		SendAppRequestF: func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, msg []byte) error {
			sentAppRequest <- msg
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

	require.NoError(client.AppRequest(ctx, set.Of(wantNodeID), []byte("request"), callback))
	<-sentAppRequest

	require.NoError(network.AppRequestFailed(ctx, wantNodeID, 1, errFoo))
	<-done
}

// Messages for unregistered handlers should be dropped gracefully
func TestAppGossipMessageForUnregisteredHandler(t *testing.T) {
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
				AppGossipF: func(context.Context, ids.NodeID, []byte) {
					require.Fail("should not be called")
				},
			}
			network, err := NewNetwork(log.NewNoOpLogger(), nil, metric.NewRegistry(), "")
			require.NoError(err)
			require.NoError(network.AddHandler(handlerID, handler))
			require.NoError(network.AppGossip(ctx, ids.EmptyNodeID, tt.msg))
		})
	}
}

// An unregistered handler should gracefully drop messages by responding
// to the requester with a consensuscore.AppError
func TestAppRequestMessageForUnregisteredHandler(t *testing.T) {
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
				AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *consensuscore.AppError) {
					require.Fail("should not be called")
					return nil, nil
				},
			}

			wantNodeID := ids.GenerateTestNodeID()
			wantRequestID := uint32(111)

			done := make(chan struct{})
			sender := &testSender{
				t: t,
				SendAppErrorF: func(_ context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
					defer close(done)

					require.Equal(wantNodeID, nodeID)
					require.Equal(wantRequestID, requestID)
					require.Equal(ErrUnregisteredHandler.Code, errorCode)
					require.Equal(ErrUnregisteredHandler.Message, errorMessage)

					return nil
				},
			}
			network, err := NewNetwork(log.NewNoOpLogger(), sender, metric.NewRegistry(), "")
			require.NoError(err)
			require.NoError(network.AddHandler(handlerID, handler))

			require.NoError(network.AppRequest(ctx, wantNodeID, wantRequestID, time.Time{}, tt.msg))
			<-done
		})
	}
}

// A handler that errors should send an AppError to the requesting peer
func TestAppError(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	appError := &consensuscore.AppError{
		Code:    123,
		Message: "foo",
	}
	handler := &TestHandler{
		AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *consensuscore.AppError) {
			return nil, appError
		},
	}

	wantNodeID := ids.GenerateTestNodeID()
	wantRequestID := uint32(111)

	done := make(chan struct{})
	sender := &testSender{
		t: t,
		SendAppErrorF: func(_ context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
			defer close(done)

			require.Equal(wantNodeID, nodeID)
			require.Equal(wantRequestID, requestID)
			require.Equal(appError.Code, errorCode)
			require.Equal(appError.Message, errorMessage)

			return nil
		},
	}
	network, err := NewNetwork(log.NewNoOpLogger(), sender, metric.NewRegistry(), "")
	require.NoError(err)
	require.NoError(network.AddHandler(handlerID, handler))
	msg := PrefixMessage(ProtocolPrefix(handlerID), []byte("message"))

	require.NoError(network.AppRequest(ctx, wantNodeID, wantRequestID, time.Time{}, msg))
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
				AppGossipF: func(context.Context, ids.NodeID, []byte) {
					require.Fail("should not be called")
				},
				AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *consensuscore.AppError) {
					require.Fail("should not be called")
					return nil, nil
				},
			}
			network, err := NewNetwork(log.NewNoOpLogger(), nil, metric.NewRegistry(), "")
			require.NoError(err)
			require.NoError(network.AddHandler(handlerID, handler))

			err = network.AppResponse(ctx, ids.EmptyNodeID, 0, []byte("foobar"))
			require.ErrorIs(err, ErrUnrequestedResponse)
			err = network.AppRequestFailed(ctx, ids.EmptyNodeID, 0, &consensuscore.AppError{Code: -1, Message: "timeout"})
			require.ErrorIs(err, ErrUnrequestedResponse)
		})
	}
}

// It's possible for the request id to overflow and wrap around.
// If there are still pending requests with the same request id, we should
// not attempt to issue another request until the previous one has cleared.
func TestAppRequestDuplicateRequestIDs(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	sentAppRequest := make(chan []byte, 1)
	sender := &testSender{
		t: t,
		SendAppRequestF: func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, msg []byte) error {
			sentAppRequest <- msg
			return nil
		},
	}

	network, err := NewNetwork(log.NewNoOpLogger(), sender, metric.NewRegistry(), "")
	require.NoError(err)
	client := network.NewClient(0x1)

	noOpCallback := func(context.Context, ids.NodeID, []byte, error) {}
	// create a request that never gets a response
	network.router.requestID = 1
	require.NoError(client.AppRequest(ctx, set.Of(ids.EmptyNodeID), []byte{}, noOpCallback))
	<-sentAppRequest

	// force the network to use the same requestID
	network.router.requestID = 1
	err = client.AppRequest(context.Background(), set.Of(ids.EmptyNodeID), []byte{}, noOpCallback)
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

func TestAppRequestAnyNodeSelection(t *testing.T) {
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
				SendAppRequestF: func(_ context.Context, nodeIDs set.Set[ids.NodeID], _ uint32, _ []byte) error {
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

			err = client.AppRequestAny(context.Background(), []byte("foobar"), nil)
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
				SendAppRequestF: func(_ context.Context, nodeIDs set.Set[ids.NodeID], _ uint32, _ []byte) error {
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

			if err = client.AppRequestAny(ctx, []byte("request"), nil); err != nil {
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
