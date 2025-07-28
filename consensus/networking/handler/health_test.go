// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/consensustest"
	"github.com/luxfi/node/consensus/engine/enginetest"
	"github.com/luxfi/node/consensus/engine/linear/block"
	"github.com/luxfi/node/consensus/networking/tracker"
	"github.com/luxfi/node/consensus/sampling"
	"github.com/luxfi/node/consensus/validators"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/network/p2p"
	"github.com/luxfi/node/subnets"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/math/meter"
	"github.com/luxfi/node/utils/resource"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/version"

	commontracker "github.com/luxfi/node/consensus/engine/core/tracker"
	p2ppb "github.com/luxfi/node/proto/pb/p2p"
)

func TestHealthCheckSubnet(t *testing.T) {
	tests := map[string]struct {
		consensusParams sampling.Parameters
	}{
		"default consensus params": {
			consensusParams: sampling.DefaultParameters,
		},
		"custom consensus params": {
			func() sampling.Parameters {
				params := sampling.DefaultParameters
				params.K = params.AlphaConfidence
				return params
			}(),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			consensusCtx := consensustest.Context(t, consensustest.CChainID)
			ctx := consensustest.ConsensusContext(consensusCtx)

			vdrs := validators.NewManager()

			resourceTracker, err := tracker.NewResourceTracker(
				prometheus.NewRegistry(),
				resource.NoUsage,
				meter.ContinuousFactory{},
				time.Second,
			)
			require.NoError(err)

			peerTracker := commontracker.NewPeers()
			vdrs.RegisterSetCallbackListener(ctx.SubnetID, peerTracker)

			sb := subnets.New(
				ctx.NodeID,
				subnets.Config{
					ConsensusParameters: test.consensusParams,
				},
			)

			p2pTracker, err := p2p.NewPeerTracker(
				logging.NoLog{},
				"",
				prometheus.NewRegistry(),
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
				sb,
				peerTracker,
				p2pTracker,
				prometheus.NewRegistry(),
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

			handlerIntf.SetEngineManager(&EngineManager{
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

			handlerIntf.Start(context.Background(), false)

			testVdrCount := 4
			vdrIDs := set.NewSet[ids.NodeID](testVdrCount)
			for i := 0; i < testVdrCount; i++ {
				vdrID := ids.GenerateTestNodeID()
				vdrIDs.Add(vdrID)

				require.NoError(vdrs.AddStaker(ctx.SubnetID, vdrID, nil, ids.Empty, 100))
			}
			vdrIDsList := vdrIDs.List()
			for index, nodeID := range vdrIDsList {
				require.NoError(peerTracker.Connected(context.Background(), nodeID, nil))

				details, err := handlerIntf.HealthCheck(context.Background())
				expectedPercentConnected := float64(index+1) / float64(testVdrCount)
				conf := sb.Config()
				minPercentConnected := conf.ConsensusParameters.MinPercentConnectedHealthy()
				if expectedPercentConnected >= minPercentConnected {
					require.NoError(err)
					continue
				}
				require.ErrorIs(err, ErrNotConnectedEnoughStake)

				detailsMap, ok := details.(map[string]interface{})
				require.True(ok)
				require.Equal(
					map[string]interface{}{
						"percentConnected":       expectedPercentConnected,
						"disconnectedValidators": set.Of(vdrIDsList[index+1:]...),
					},
					detailsMap["networking"],
				)
			}
		})
	}
}
