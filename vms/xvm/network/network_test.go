// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/engine/core"
	"github.com/luxfi/node/quasar/engine/core/coremock"
	"github.com/luxfi/node/quasar/validators"
	log "github.com/luxfi/log"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/nftfx"
	"github.com/luxfi/node/vms/propertyfx"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/vms/txs/mempool"
	"github.com/luxfi/node/vms/xvm/block/executor/executormock"
	"github.com/luxfi/node/vms/xvm/fxs"
	"github.com/luxfi/node/vms/xvm/txs"

	xmempool "github.com/luxfi/node/vms/xvm/txs/mempool"
)

var (
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

	errTest = errors.New("test error")
)

func TestNetworkIssueTxFromRPC(t *testing.T) {
	type test struct {
		name           string
		mempool        mempool.Mempool[*txs.Tx]
		txVerifierFunc func(*gomock.Controller) TxVerifier
		appSenderFunc  func(*gomock.Controller) core.AppSender
		tx             *txs.Tx
		expectedErr    error
	}

	tests := []test{
		{
			name: "mempool has transaction",
			mempool: func() mempool.Mempool[*txs.Tx] {
				mempool, err := xmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				require.NoError(t, mempool.Add(&txs.Tx{Unsigned: &txs.BaseTx{}}))
				return mempool
			}(),
			tx:          &txs.Tx{Unsigned: &txs.BaseTx{}},
			expectedErr: mempool.ErrDuplicateTx,
		},
		{
			name: "transaction marked as dropped in mempool",
			mempool: func() mempool.Mempool[*txs.Tx] {
				mempool, err := xmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				mempool.MarkDropped(ids.Empty, errTest)
				return mempool
			}(),
			tx:          &txs.Tx{Unsigned: &txs.BaseTx{}},
			expectedErr: errTest,
		},
		{
			name: "tx too big",
			mempool: func() mempool.Mempool[*txs.Tx] {
				mempool, err := xmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				return mempool
			}(),
			txVerifierFunc: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executormock.NewManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return txVerifier
			},
			tx: func() *txs.Tx {
				tx := &txs.Tx{Unsigned: &txs.BaseTx{}}
				bytes := make([]byte, mempool.MaxTxSize+1)
				tx.SetBytes(bytes, bytes)
				return tx
			}(),
			expectedErr: mempool.ErrTxTooLarge,
		},
		{
			name: "tx conflicts",
			mempool: func() mempool.Mempool[*txs.Tx] {
				mempool, err := xmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)

				tx := &txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: lux.BaseTx{
							Ins: []*lux.TransferableInput{
								{
									UTXOID: lux.UTXOID{},
								},
							},
						},
					},
				}

				require.NoError(t, mempool.Add(tx))
				return mempool
			}(),
			txVerifierFunc: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executormock.NewManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return txVerifier
			},
			tx: func() *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: lux.BaseTx{
							Ins: []*lux.TransferableInput{
								{
									UTXOID: lux.UTXOID{},
								},
							},
						},
					},
					TxID: ids.ID{1},
				}
				return tx
			}(),
			expectedErr: mempool.ErrConflictsWithOtherTx,
		},
		{
			name: "mempool full",
			mempool: func() mempool.Mempool[*txs.Tx] {
				m, err := xmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)

				for i := 0; i < 1024; i++ {
					tx := &txs.Tx{Unsigned: &txs.BaseTx{}}
					bytes := make([]byte, mempool.MaxTxSize)
					tx.SetBytes(bytes, bytes)
					tx.TxID = ids.GenerateTestID()
					require.NoError(t, m.Add(tx))
				}

				return m
			}(),
			txVerifierFunc: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executormock.NewManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return txVerifier
			},
			tx: func() *txs.Tx {
				tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: lux.BaseTx{}}}
				tx.SetBytes([]byte{1, 2, 3}, []byte{1, 2, 3})
				return tx
			}(),
			expectedErr: mempool.ErrMempoolFull,
		},
		{
			name: "happy path",
			mempool: func() mempool.Mempool[*txs.Tx] {
				mempool, err := xmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				return mempool
			}(),
			txVerifierFunc: func(ctrl *gomock.Controller) TxVerifier {
				txVerifier := executormock.NewManager(ctrl)
				txVerifier.EXPECT().VerifyTx(gomock.Any()).Return(nil)
				return txVerifier
			},
			appSenderFunc: func(ctrl *gomock.Controller) core.AppSender {
				appSender := coremock.NewMockAppSender(ctrl)
				appSender.EXPECT().SendAppGossip(gomock.Any(), gomock.Any()).Return(nil)
				return appSender
			},
			tx:          &txs.Tx{Unsigned: &txs.BaseTx{}},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			parser, err := txs.NewParser(
				[]fxs.Fx{
					&secp256k1fx.Fx{},
					&nftfx.Fx{},
					&propertyfx.Fx{},
				},
			)
			require.NoError(err)

			txVerifierFunc := func(ctrl *gomock.Controller) TxVerifier {
				return executormock.NewManager(ctrl)
			}
			if tt.txVerifierFunc != nil {
				txVerifierFunc = tt.txVerifierFunc
			}

			appSenderFunc := func(ctrl *gomock.Controller) core.AppSender {
				return coremock.NewMockAppSender(ctrl)
			}
			if tt.appSenderFunc != nil {
				appSenderFunc = tt.appSenderFunc
			}

			n, err := New(
				log.NewNoOpLogger(),
				ids.EmptyNodeID,
				ids.Empty,
				&mockValidatorsState{
					GetCurrentHeightF: func(context.Context) (uint64, error) {
						return 0, nil
					},
					GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
						return nil, nil
					},
				},
				parser,
				txVerifierFunc(ctrl),
				tt.mempool,
				appSenderFunc(ctrl),
				prometheus.NewRegistry(),
				testConfig,
			)
			require.NoError(err)
			err = n.IssueTxFromRPC(tt.tx)
			require.ErrorIs(err, tt.expectedErr)

			require.NoError(n.txPushGossiper.Gossip(context.Background()))
		})
	}
}

func TestNetworkIssueTxFromRPCWithoutVerification(t *testing.T) {
	type test struct {
		name          string
		mempool       mempool.Mempool[*txs.Tx]
		appSenderFunc func(*gomock.Controller) core.AppSender
		expectedErr   error
	}

	tests := []test{
		{
			name: "happy path",
			mempool: func() mempool.Mempool[*txs.Tx] {
				mempool, err := xmempool.New("", prometheus.NewRegistry())
				require.NoError(t, err)
				return mempool
			}(),
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

			parser, err := txs.NewParser(
				[]fxs.Fx{
					&secp256k1fx.Fx{},
					&nftfx.Fx{},
					&propertyfx.Fx{},
				},
			)
			require.NoError(err)

			appSenderFunc := func(ctrl *gomock.Controller) core.AppSender {
				return coremock.NewMockAppSender(ctrl)
			}
			if tt.appSenderFunc != nil {
				appSenderFunc = tt.appSenderFunc
			}

			n, err := New(
				log.NewNoOpLogger(),
				ids.EmptyNodeID,
				ids.Empty,
				&mockValidatorsState{
					GetCurrentHeightF: func(context.Context) (uint64, error) {
						return 0, nil
					},
					GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
						return nil, nil
					},
				},
				parser,
				executormock.NewManager(ctrl), // Should never verify a tx
				tt.mempool,
				appSenderFunc(ctrl),
				prometheus.NewRegistry(),
				testConfig,
			)
			require.NoError(err)
			err = n.IssueTxFromRPCWithoutVerification(&txs.Tx{Unsigned: &txs.BaseTx{}})
			require.ErrorIs(err, tt.expectedErr)

			require.NoError(n.txPushGossiper.Gossip(context.Background()))
		})
	}
}
