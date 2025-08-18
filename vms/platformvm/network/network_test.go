// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/luxfi/mock/gomock"

	"github.com/luxfi/consensus"
	"github.com/luxfi/consensus/consensustest"
	"github.com/luxfi/consensus/core"
	"github.com/luxfi/consensus/core/coremock"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/txs/mempool"

	pmempool "github.com/luxfi/node/vms/platformvm/txs/mempool"
)

var (
	errTest = errors.New("test error")

	testConfig = Config{
		MaxValidatorSetStaleness:                    time.Second,
		TargetGossipSize:                            1,
		PushGossipNumValidators:                     1,
		PushGossipNumPeers:                          0,
		PushRegossipNumValidators:                   1,
		PushRegossipNumPeers:                        0,
		PushGossipDiscardedCacheSize:                1,
		PushGossipMaxRegossipFrequency:              time.Second,
		PushGossipFrequency:                         time.Second,
		PullGossipPollSize:                          1,
		PullGossipFrequency:                         time.Second,
		PullGossipThrottlingPeriod:                  time.Second,
		PullGossipThrottlingLimit:                   1,
		ExpectedBloomFilterElements:                 10,
		ExpectedBloomFilterFalsePositiveProbability: .1,
		MaxBloomFilterFalsePositiveProbability:      .5,
	}
)

// mockValidatorState implements validators.State for testing
type mockValidatorState struct {
	height     uint64
	validators map[ids.NodeID]*validators.GetValidatorOutput
}

func (m *mockValidatorState) GetMinimumHeight(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (m *mockValidatorState) GetCurrentHeight(ctx context.Context) (uint64, error) {
	return m.height, nil
}

func (m *mockValidatorState) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	return ids.Empty, nil
}

func (m *mockValidatorState) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return m.validators, nil
}

func (m *mockValidatorState) GetCurrentValidatorSet(
	ctx context.Context,
	subnetID ids.ID,
) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
	// Not used in this test
	return nil, m.height, nil
}

var _ TxVerifier = (*testTxVerifier)(nil)

type testTxVerifier struct {
	err error
}

func (t testTxVerifier) VerifyTx(*txs.Tx) error {
	return t.err
}

func TestNetworkIssueTxFromRPC(t *testing.T) {
	tx := &txs.Tx{}

	type test struct {
		name                      string
		mempoolFunc               func(*gomock.Controller) pmempool.Mempool
		txVerifier                testTxVerifier
		partialSyncPrimaryNetwork bool
		appSenderFunc             func(*gomock.Controller) core.AppSender
		expectedErr               error
	}

	tests := []test{
		{
			name: "mempool has transaction",
			mempoolFunc: func(ctrl *gomock.Controller) pmempool.Mempool {
				mempool := pmempool.NewMockMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(tx, true)
				return mempool
			},
			appSenderFunc: func(ctrl *gomock.Controller) core.AppSender {
				return &core.FakeSender{}
			},
			expectedErr: mempool.ErrDuplicateTx,
		},
		{
			name: "transaction marked as dropped in mempool",
			mempoolFunc: func(ctrl *gomock.Controller) pmempool.Mempool {
				mempool := pmempool.NewMockMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(errTest)
				return mempool
			},
			appSenderFunc: func(ctrl *gomock.Controller) core.AppSender {
				// Shouldn't gossip the tx
				return &core.FakeSender{}
			},
			expectedErr: errTest,
		},
		{
			name: "transaction invalid",
			mempoolFunc: func(ctrl *gomock.Controller) pmempool.Mempool {
				mempool := pmempool.NewMockMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(nil)
				mempool.EXPECT().MarkDropped(gomock.Any(), gomock.Any())
				return mempool
			},
			txVerifier: testTxVerifier{err: errTest},
			appSenderFunc: func(ctrl *gomock.Controller) core.AppSender {
				// Shouldn't gossip the tx
				return &core.FakeSender{}
			},
			expectedErr: errTest,
		},
		{
			name: "can't add transaction to mempool",
			mempoolFunc: func(ctrl *gomock.Controller) pmempool.Mempool {
				mempool := pmempool.NewMockMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(nil)
				mempool.EXPECT().Add(gomock.Any()).Return(errTest)
				mempool.EXPECT().MarkDropped(gomock.Any(), gomock.Any())
				return mempool
			},
			appSenderFunc: func(ctrl *gomock.Controller) core.AppSender {
				// Shouldn't gossip the tx
				return &core.FakeSender{}
			},
			expectedErr: errTest,
		},
		{
			name: "mempool is disabled if primary network is not being fully synced",
			mempoolFunc: func(ctrl *gomock.Controller) pmempool.Mempool {
				return pmempool.NewMockMempool(ctrl)
			},
			partialSyncPrimaryNetwork: true,
			appSenderFunc: func(ctrl *gomock.Controller) core.AppSender {
				return &core.FakeSender{}
			},
			expectedErr: errMempoolDisabledWithPartialSync,
		},
		{
			name: "happy path",
			mempoolFunc: func(ctrl *gomock.Controller) pmempool.Mempool {
				mempool := pmempool.NewMockMempool(ctrl)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, false)
				mempool.EXPECT().GetDropReason(gomock.Any()).Return(nil)
				mempool.EXPECT().Add(gomock.Any()).Return(nil)
				mempool.EXPECT().Len().Return(0)
				mempool.EXPECT().RequestBuildBlock(false)
				mempool.EXPECT().Get(gomock.Any()).Return(nil, true).Times(2)
				return mempool
			},
			appSenderFunc: func(ctrl *gomock.Controller) core.AppSender {
				appSender := coremock.NewMockAppSender(ctrl)
				appSender.EXPECT().SendAppGossip(gomock.Any(), gomock.Any()).Return(nil)
				return appSender
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			consensusCtx := consensustest.Context(t, ids.Empty)
			// Extract values from context
			nodeID := consensus.GetNodeID(consensusCtx)
			subnetID := consensus.GetSubnetID(consensusCtx)
			// Use a simple test logger for now
			logger := log.NoLog{}
			// Create a mock validator state that returns sensible defaults
			validatorState := &mockValidatorState{
				height: 100,
				validators: map[ids.NodeID]*validators.GetValidatorOutput{
					nodeID: {
						NodeID:    nodeID,
						PublicKey: nil,
						Weight:    100,
					},
				},
			}
			n, err := New(
				logger,
				nodeID,
				subnetID,
				validatorState,
				tt.txVerifier,
				tt.mempoolFunc(ctrl),
				tt.partialSyncPrimaryNetwork,
				tt.appSenderFunc(ctrl),
				prometheus.NewRegistry(),
				testConfig,
			)
			require.NoError(err)

			err = n.IssueTxFromRPC(tx)
			require.ErrorIs(err, tt.expectedErr)

			require.NoError(n.txPushGossiper.Gossip(context.Background()))
		})
	}
}
