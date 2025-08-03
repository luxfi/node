// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/luxfi/metrics"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/consensustest"
	enginepkg "github.com/luxfi/node/consensus/engine/core"
	"github.com/luxfi/node/consensus/engine/enginetest"
	"github.com/luxfi/node/consensus/engine/chain/block"
	"github.com/luxfi/node/consensus/networking/tracker"
	"github.com/luxfi/node/consensus/validators"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network/p2p"
	"github.com/luxfi/node/subnets"
	"github.com/luxfi/log"
	"github.com/luxfi/node/utils/math/meter"
	"github.com/luxfi/node/utils/resource"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/version"

	commontracker "github.com/luxfi/node/consensus/engine/core/tracker"
	p2ppb "github.com/luxfi/node/proto/pb/p2p"
)

const testThreadPoolSize = 2

var errFatal = errors.New("error should cause handler to close")

func TestHandlerDropsTimedOutMessages(t *testing.T) {
	require := require.New(t)

	called := make(chan struct{})

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)

	vdrs := validators.NewManager()
	vdr0 := ids.GenerateTestNodeID()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, vdr0, nil, ids.Empty, 1))

	resourceTracker, err := tracker.NewResourceTracker(
		metrics.NewNoOpMetrics("test").Registry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	peerTracker, err := p2p.NewPeerTracker(
		log.NewNoOpLogger(),
		"",
		metrics.NewNoOpMetrics("test").Registry(),
		nil,
		version.CurrentApp,
	)
	require.NoError(err)

	subscription, _ := createSubscriber()

	handlerIntf, err := New(
		ctx,
		&block.ChangeNotifier{},
		subscription,
		vdrs,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		peerTracker,
		metrics.NewNoOpMetrics("test").Registry(),
		func() {},
	)
	require.NoError(err)
	handler := handlerIntf.(*handler)

	bootstrapper := &enginetest.Bootstrapper{
		Engine: enginetest.Engine{
			T: t,
		},
	}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *consensus.Context {
		return ctx
	}
	bootstrapper.GetAcceptedFrontierF = func(context.Context, ids.NodeID, uint32) error {
		require.FailNow("GetAcceptedFrontier message should have timed out")
		return nil
	}
	bootstrapper.GetAcceptedF = func(context.Context, ids.NodeID, uint32, set.Set[ids.ID]) error {
		called <- struct{}{}
		return nil
	}
	handler.SetEngineManager(&EngineManager{
		Chain: &Engine{
			Bootstrapper: bootstrapper,
		},
	})
	ctx.State.Set(consensus.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_LINEAR,
		State: consensus.Bootstrapping, // assumed bootstrap is ongoing
	})

	pastTime := time.Now()
	handler.clock.Set(pastTime)

	nodeID := ids.EmptyNodeID
	reqID := uint32(1)
	chainID := ids.Empty
	msg := Message{
		InboundMessage: message.InboundGetAcceptedFrontier(chainID, reqID, 0*time.Second, nodeID),
		EngineType:     p2ppb.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}
	handler.Push(context.Background(), msg)

	currentTime := time.Now().Add(time.Second)
	handler.clock.Set(currentTime)

	reqID++
	msg = Message{
		InboundMessage: message.InboundGetAccepted(chainID, reqID, 1*time.Second, nil, nodeID),
		EngineType:     p2ppb.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}
	handler.Push(context.Background(), msg)

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}

	handler.Start(context.Background(), false)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	select {
	case <-ticker.C:
		require.FailNow("Calling engine function timed out")
	case <-called:
	}
}

func TestHandlerClosesOnError(t *testing.T) {
	require := require.New(t)

	closed := make(chan struct{}, 1)
	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	t.Logf("Test starting with subnet %s", ctx.SubnetID)

	vdrs := validators.NewManager()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	resourceTracker, err := tracker.NewResourceTracker(
		metrics.NewNoOpMetrics("test").Registry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	peerTracker, err := p2p.NewPeerTracker(
		log.NewNoOpLogger(),
		"",
		metrics.NewNoOpMetrics("test").Registry(),
		nil,
		version.CurrentApp,
	)
	require.NoError(err)

	var cn block.ChangeNotifier
	subscription := func(context.Context) (enginepkg.Message, error) {
		return enginepkg.PendingTxs, nil
	}

	handlerIntf, err := New(
		ctx,
		&cn,
		subscription,
		vdrs,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		peerTracker,
		metrics.NewNoOpMetrics("test").Registry(),
		func() {},
	)
	require.NoError(err)
	handler := handlerIntf.(*handler)

	handler.clock.Set(time.Now())
	handler.SetOnStopped(func() {
		closed <- struct{}{}
	})

	bootstrapper := &enginetest.Bootstrapper{
		Engine: enginetest.Engine{
			T: t,
		},
	}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *consensus.Context {
		return ctx
	}
	bootstrapper.GetAcceptedFrontierF = func(context.Context, ids.NodeID, uint32) error {
		return errFatal
	}

	engine := &enginetest.Engine{T: t}
	engine.Default(false)
	engine.ContextF = func() *consensus.Context {
		return ctx
	}

	handler.SetEngineManager(&EngineManager{
		Chain: &Engine{
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})

	// assume bootstrapping is ongoing so that InboundGetAcceptedFrontier
	// should normally be handled
	ctx.State.Set(consensus.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_LINEAR,
		State: consensus.Bootstrapping,
	})

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}

	handler.Start(context.Background(), false)

	nodeID := ids.EmptyNodeID
	reqID := uint32(1)
	deadline := time.Nanosecond
	msg := Message{
		InboundMessage: message.InboundGetAcceptedFrontier(ids.Empty, reqID, deadline, nodeID),
		EngineType:     p2ppb.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}
	handler.Push(context.Background(), msg)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	select {
	case <-ticker.C:
		require.FailNow("Handler shutdown timed out before calling toClose")
	case <-closed:
	}
}

func TestHandlerDropsGossipDuringBootstrapping(t *testing.T) {
	require := require.New(t)

	closed := make(chan struct{}, 1)
	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	vdrs := validators.NewManager()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	resourceTracker, err := tracker.NewResourceTracker(
		metrics.NewNoOpMetrics("test").Registry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	peerTracker, err := p2p.NewPeerTracker(
		log.NewNoOpLogger(),
		"",
		metrics.NewNoOpMetrics("test").Registry(),
		nil,
		version.CurrentApp,
	)
	require.NoError(err)

	subscription, _ := createSubscriber()

	handlerIntf, err := New(
		ctx,
		&block.ChangeNotifier{},
		subscription,
		vdrs,
		1,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		peerTracker,
		metrics.NewNoOpMetrics("test").Registry(),
		func() {},
	)
	require.NoError(err)
	handler := handlerIntf.(*handler)

	handler.clock.Set(time.Now())

	bootstrapper := &enginetest.Bootstrapper{
		Engine: enginetest.Engine{
			T: t,
		},
	}
	bootstrapper.Default(false)
	bootstrapper.ContextF = func() *consensus.Context {
		return ctx
	}
	bootstrapper.GetFailedF = func(context.Context, ids.NodeID, uint32) error {
		closed <- struct{}{}
		return nil
	}
	handler.SetEngineManager(&EngineManager{
		Chain: &Engine{
			Bootstrapper: bootstrapper,
		},
	})
	ctx.State.Set(consensus.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_LINEAR,
		State: consensus.Bootstrapping, // assumed bootstrap is ongoing
	})

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}

	handler.Start(context.Background(), false)

	nodeID := ids.EmptyNodeID
	chainID := ids.Empty
	reqID := uint32(1)
	inInboundMessage := Message{
		InboundMessage: message.InternalGetFailed(nodeID, chainID, reqID),
		EngineType:     p2ppb.EngineType_ENGINE_TYPE_UNSPECIFIED,
	}
	handler.Push(context.Background(), inInboundMessage)

	ticker := time.NewTicker(time.Second)
	select {
	case <-ticker.C:
		require.FailNow("Handler shutdown timed out before calling toClose")
	case <-closed:
	}
}

// Test that messages from the VM are handled
func TestHandlerDispatchInternal(t *testing.T) {
	require := require.New(t)

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	vdrs := validators.NewManager()
	require.NoError(vdrs.AddStaker(ctx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	resourceTracker, err := tracker.NewResourceTracker(
		metrics.NewNoOpMetrics("test").Registry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	peerTracker, err := p2p.NewPeerTracker(
		log.NewNoOpLogger(),
		"",
		metrics.NewNoOpMetrics("test").Registry(),
		nil,
		version.CurrentApp,
	)
	require.NoError(err)

	subscription, messages := createSubscriber()
	notified := make(chan enginepkg.Message)

	handler, err := New(
		ctx,
		&block.ChangeNotifier{},
		subscription,
		vdrs,
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		peerTracker,
		metrics.NewNoOpMetrics("test").Registry(),
		func() {},
	)
	require.NoError(err)

	bootstrapper := &enginetest.Bootstrapper{
		Engine: enginetest.Engine{
			T: t,
		},
	}
	bootstrapper.Default(false)

	engine := &enginetest.Engine{T: t}
	engine.Default(false)
	engine.ContextF = func() *consensus.Context {
		return ctx
	}

	engine.NotifyF = func(ctx context.Context, msg enginepkg.Message) error {
		select {
		case notified <- msg:
			notified <- msg
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	handler.SetEngineManager(&EngineManager{
		Chain: &Engine{
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})

	ctx.State.Set(consensus.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_LINEAR,
		State: consensus.NormalOp, // assumed bootstrap is done
	})

	bootstrapper.StartF = func(context.Context, uint32) error {
		return nil
	}

	handler.Start(context.Background(), false)
	messages <- enginepkg.PendingTxs
	select {
	case msg := <-notified:
		require.Equal(enginepkg.PendingTxs, msg)
	case <-time.After(time.Minute):
		require.FailNow("Handler did not dispatch expected message")
	}
}

// Tests that messages are routed to the correct engine type
func TestDynamicEngineTypeDispatch(t *testing.T) {
	tests := []struct {
		name                string
		currentEngineType   p2ppb.EngineType
		requestedEngineType p2ppb.EngineType
		setup               func(
			h Handler,
			b enginepkg.BootstrapableEngine,
			e enginepkg.Engine,
		)
	}{
		{
			name:                "current - lux, requested - unspecified",
			currentEngineType:   p2ppb.EngineType_ENGINE_TYPE_LUX,
			requestedEngineType: p2ppb.EngineType_ENGINE_TYPE_UNSPECIFIED,
			setup: func(h Handler, b enginepkg.BootstrapableEngine, e enginepkg.Engine) {
				h.SetEngineManager(&EngineManager{
					Dag: &Engine{
						StateSyncer:  nil,
						Bootstrapper: b,
						Consensus:    e,
					},
					Chain: nil,
				})
			},
		},
		{
			name:                "current - lux, requested - lux",
			currentEngineType:   p2ppb.EngineType_ENGINE_TYPE_LUX,
			requestedEngineType: p2ppb.EngineType_ENGINE_TYPE_LUX,
			setup: func(h Handler, b enginepkg.BootstrapableEngine, e enginepkg.Engine) {
				h.SetEngineManager(&EngineManager{
					Dag: &Engine{
						StateSyncer:  nil,
						Bootstrapper: b,
						Consensus:    e,
					},
					Chain: nil,
				})
			},
		},
		{
			name:                "current - linear, requested - unspecified",
			currentEngineType:   p2ppb.EngineType_ENGINE_TYPE_LINEAR,
			requestedEngineType: p2ppb.EngineType_ENGINE_TYPE_UNSPECIFIED,
			setup: func(h Handler, b enginepkg.BootstrapableEngine, e enginepkg.Engine) {
				h.SetEngineManager(&EngineManager{
					Dag: nil,
					Chain: &Engine{
						StateSyncer:  nil,
						Bootstrapper: b,
						Consensus:    e,
					},
				})
			},
		},
		{
			name:                "current - linear, requested - lux",
			currentEngineType:   p2ppb.EngineType_ENGINE_TYPE_LINEAR,
			requestedEngineType: p2ppb.EngineType_ENGINE_TYPE_LUX,
			setup: func(h Handler, b enginepkg.BootstrapableEngine, e enginepkg.Engine) {
				h.SetEngineManager(&EngineManager{
					Dag: &Engine{
						StateSyncer:  nil,
						Bootstrapper: nil,
						Consensus:    e,
					},
					Chain: &Engine{
						StateSyncer:  nil,
						Bootstrapper: b,
						Consensus:    nil,
					},
				})
			},
		},
		{
			name:                "current - linear, requested - linear",
			currentEngineType:   p2ppb.EngineType_ENGINE_TYPE_LINEAR,
			requestedEngineType: p2ppb.EngineType_ENGINE_TYPE_LINEAR,
			setup: func(h Handler, b enginepkg.BootstrapableEngine, e enginepkg.Engine) {
				h.SetEngineManager(&EngineManager{
					Dag: nil,
					Chain: &Engine{
						StateSyncer:  nil,
						Bootstrapper: b,
						Consensus:    e,
					},
				})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			messageReceived := make(chan struct{})
			consensusCtx := consensustest.Context(t, consensustest.CChainID)
			ctx := consensustest.ConsensusContext(consensusCtx)
			vdrs := validators.NewManager()
			require.NoError(vdrs.AddStaker(ctx.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))

			resourceTracker, err := tracker.NewResourceTracker(
				metrics.NewNoOpMetrics("test").Registry(),
				resource.NoUsage,
				meter.ContinuousFactory{},
				time.Second,
			)
			require.NoError(err)

			peerTracker, err := p2p.NewPeerTracker(
				log.NewNoOpLogger(),
				"",
				metrics.NewNoOpMetrics("test").Registry(),
				nil,
				version.CurrentApp,
			)
			require.NoError(err)

			subscription, _ := createSubscriber()

			handler, err := New(
				ctx,
				&block.ChangeNotifier{},
				subscription,
				vdrs,
				time.Second,
				testThreadPoolSize,
				resourceTracker,
				subnets.New(ids.EmptyNodeID, subnets.Config{}),
				commontracker.NewPeers(),
				peerTracker,
				metrics.NewNoOpMetrics("test").Registry(),
				func() {},
			)
			require.NoError(err)

			bootstrapper := &enginetest.Bootstrapper{
				Engine: enginetest.Engine{
					T: t,
				},
			}
			bootstrapper.Default(false)

			engine := &enginetest.Engine{T: t}
			engine.Default(false)
			engine.ContextF = func() *consensus.Context {
				return ctx
			}
			engine.ChitsF = func(context.Context, ids.NodeID, uint32, ids.ID, ids.ID, ids.ID, uint64) error {
				close(messageReceived)
				return nil
			}

			test.setup(handler, bootstrapper, engine)

			ctx.State.Set(consensus.EngineState{
				Type:  test.currentEngineType,
				State: consensus.NormalOp, // assumed bootstrap is done
			})

			bootstrapper.StartF = func(context.Context, uint32) error {
				return nil
			}

			handler.Start(context.Background(), false)
			handler.Push(context.Background(), Message{
				InboundMessage: message.InboundChits(
					ids.Empty,
					uint32(0),
					ids.Empty,
					ids.Empty,
					ids.Empty,
					ids.EmptyNodeID,
				),
				EngineType: test.requestedEngineType,
			})

			<-messageReceived
		})
	}
}

func TestHandlerStartError(t *testing.T) {
	require := require.New(t)

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	resourceTracker, err := tracker.NewResourceTracker(
		metrics.NewNoOpMetrics("test").Registry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	peerTracker, err := p2p.NewPeerTracker(
		log.NewNoOpLogger(),
		"",
		metrics.NewNoOpMetrics("test").Registry(),
		nil,
		version.CurrentApp,
	)
	require.NoError(err)

	subscription, _ := createSubscriber()

	handler, err := New(
		ctx,
		&block.ChangeNotifier{},
		subscription,
		validators.NewManager(),
		time.Second,
		testThreadPoolSize,
		resourceTracker,
		subnets.New(ctx.NodeID, subnets.Config{}),
		commontracker.NewPeers(),
		peerTracker,
		metrics.NewNoOpMetrics("test").Registry(),
		func() {},
	)
	require.NoError(err)

	// Starting a handler with an unprovided engine should immediately cause the
	// handler to shutdown.
	handler.SetEngineManager(&EngineManager{})
	ctx.State.Set(consensus.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_LINEAR,
		State: consensus.Initializing,
	})
	handler.Start(context.Background(), false)

	_, err = handler.AwaitStopped(context.Background())
	require.NoError(err)
}

func createSubscriber() (enginepkg.Subscription, chan<- enginepkg.Message) {
	messages := make(chan enginepkg.Message, 1)

	subscription := func(ctx context.Context) (enginepkg.Message, error) {
		select {
		case msg := <-messages:
			return msg, nil
		case <-ctx.Done():
			return enginepkg.Message(0), ctx.Err()
		}
	}

	return subscription, messages
}
