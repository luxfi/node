// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/luxfi/mock/gomock"

	"github.com/luxfi/consensus/choices"
	"github.com/luxfi/consensus/consensustest"
	"github.com/luxfi/consensus/uptime"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/status"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/txs/executor"
)

// mockCalculator is a simple mock for uptime.Calculator
type mockCalculator struct {
	calculateUptimeF func(ids.NodeID, ids.ID) (time.Duration, time.Duration, error)
	calculateUptimePercentF func(ids.NodeID, ids.ID) (float64, error)
	calculateUptimePercentFromF func(ids.NodeID, ids.ID, time.Time) (float64, error)
	setCalculateUptimeF func(ids.NodeID, ids.ID, time.Duration)
}

func (m *mockCalculator) CalculateUptime(nodeID ids.NodeID, subnetID ids.ID) (time.Duration, time.Duration, error) {
	if m.calculateUptimeF != nil {
		return m.calculateUptimeF(nodeID, subnetID)
	}
	return 0, 0, nil
}

func (m *mockCalculator) CalculateUptimePercent(nodeID ids.NodeID, subnetID ids.ID) (float64, error) {
	if m.calculateUptimePercentF != nil {
		return m.calculateUptimePercentF(nodeID, subnetID)
	}
	return 1.0, nil
}

func (m *mockCalculator) CalculateUptimePercentFrom(nodeID ids.NodeID, subnetID ids.ID, startTime time.Time) (float64, error) {
	if m.calculateUptimePercentFromF != nil {
		return m.calculateUptimePercentFromF(nodeID, subnetID, startTime)
	}
	return 1.0, nil
}

func (m *mockCalculator) SetCalculateUptime(nodeID ids.NodeID, subnetID ids.ID, upDuration time.Duration) {
	if m.setCalculateUptimeF != nil {
		m.setCalculateUptimeF(nodeID, subnetID, upDuration)
	}
}

func (m *mockCalculator) SetCalculator(ids.ID, uptime.Calculator) error {
	// Mock implementation - do nothing
	return nil
}

func TestStatus(t *testing.T) {
	type test struct {
		name           string
		blockF         func(*gomock.Controller) *Block
		expectedStatus choices.Status
	}

	tests := []test{
		{
			name: "last accepted",
			blockF: func(ctrl *gomock.Controller) *Block {
				blkID := ids.GenerateTestID()
				statelessBlk := block.NewMockBlock(ctrl)
				statelessBlk.EXPECT().ID().Return(blkID)

				manager := &manager{
					backend: &backend{
						lastAccepted: blkID,
					},
				}

				return &Block{
					Block:   statelessBlk,
					manager: manager,
				}
			},
			expectedStatus: choices.Accepted,
		},
		{
			name: "processing",
			blockF: func(ctrl *gomock.Controller) *Block {
				blkID := ids.GenerateTestID()
				statelessBlk := block.NewMockBlock(ctrl)
				statelessBlk.EXPECT().ID().Return(blkID)

				manager := &manager{
					backend: &backend{
						blkIDToState: map[ids.ID]*blockState{
							blkID: {},
						},
					},
				}
				return &Block{
					Block:   statelessBlk,
					manager: manager,
				}
			},
			expectedStatus: choices.Processing,
		},
		{
			name: "in database",
			blockF: func(ctrl *gomock.Controller) *Block {
				blkID := ids.GenerateTestID()
				statelessBlk := block.NewMockBlock(ctrl)
				statelessBlk.EXPECT().ID().Return(blkID)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetStatelessBlock(blkID).Return(statelessBlk, nil)

				manager := &manager{
					backend: &backend{
						state: state,
					},
				}
				return &Block{
					Block:   statelessBlk,
					manager: manager,
				}
			},
			expectedStatus: choices.Accepted,
		},
		{
			name: "not in map or database",
			blockF: func(ctrl *gomock.Controller) *Block {
				blkID := ids.GenerateTestID()
				statelessBlk := block.NewMockBlock(ctrl)
				statelessBlk.EXPECT().ID().Return(blkID)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetStatelessBlock(blkID).Return(nil, database.ErrNotFound)

				manager := &manager{
					backend: &backend{
						state: state,
					},
				}
				return &Block{
					Block:   statelessBlk,
					manager: manager,
				}
			},
			expectedStatus: choices.Processing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			blk := tt.blockF(ctrl)
			require.Equal(t, tt.expectedStatus, blk.Status())
		})
	}
}

func TestBlockOptions(t *testing.T) {
	type test struct {
		name                   string
		blkF                   func(*gomock.Controller) *Block
		expectedPreferenceType block.Block
	}

	tests := []test{
		{
			name: "apricot proposal block; commit preferred",
			blkF: func(ctrl *gomock.Controller) *Block {
				state := state.NewMockState(ctrl)

				uptimes := &mockCalculator{}

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   context.Background(),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Config{
							UptimePercentage: 0,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block:   &block.ApricotProposalBlock{},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.ApricotCommitBlock{},
		},
		{
			name: "banff proposal block; invalid proposal tx",
			blkF: func(ctrl *gomock.Controller) *Block {
				state := state.NewMockState(ctrl)

				uptimes := &mockCalculator{}

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   context.Background(),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Config{
							UptimePercentage: 0,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.CreateChainTx{},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; missing tx",
			blkF: func(ctrl *gomock.Controller) *Block {
				stakerTxID := ids.GenerateTestID()

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(nil, status.Unknown, database.ErrNotFound)

				uptimes := &mockCalculator{}

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   context.Background(),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Config{
							UptimePercentage: 0,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardValidatorTx{
									TxID: stakerTxID,
								},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; error fetching staker tx",
			blkF: func(ctrl *gomock.Controller) *Block {
				stakerTxID := ids.GenerateTestID()

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(nil, status.Unknown, database.ErrClosed)

				uptimes := &mockCalculator{}

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   context.Background(),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Config{
							UptimePercentage: 0,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardValidatorTx{
									TxID: stakerTxID,
								},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; unexpected staker tx type",
			blkF: func(ctrl *gomock.Controller) *Block {
				stakerTxID := ids.GenerateTestID()
				stakerTx := &txs.Tx{
					Unsigned: &txs.CreateChainTx{},
				}

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(stakerTx, status.Committed, nil)

				uptimes := &mockCalculator{}

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   context.Background(),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Config{
							UptimePercentage: 0,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardValidatorTx{
									TxID: stakerTxID,
								},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; missing primary network validator",
			blkF: func(ctrl *gomock.Controller) *Block {
				var (
					stakerTxID = ids.GenerateTestID()
					nodeID     = ids.GenerateTestNodeID()
					netID   = ids.GenerateTestID()
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddPermissionlessValidatorTx{
							Validator: txs.Validator{
								NodeID: nodeID,
							},
							Net: netID,
						},
					}
				)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(stakerTx, status.Committed, nil)
				state.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, nodeID).Return(nil, database.ErrNotFound)

				uptimes := &mockCalculator{}

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   context.Background(),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Config{
							UptimePercentage: 0,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardValidatorTx{
									TxID: stakerTxID,
								},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; failed calculating primary network uptime",
			blkF: func(ctrl *gomock.Controller) *Block {
				var (
					stakerTxID = ids.GenerateTestID()
					nodeID     = ids.GenerateTestNodeID()
					netID   = constants.PrimaryNetworkID
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddPermissionlessValidatorTx{
							Validator: txs.Validator{
								NodeID: nodeID,
							},
							Net: netID,
						},
					}
					primaryNetworkValidatorStartTime = time.Now()
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
					}
				)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(stakerTx, status.Committed, nil)
				state.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, nodeID).Return(staker, nil)

				uptimes := &mockCalculator{
					calculateUptimePercentFromF: func(ids.NodeID, ids.ID, time.Time) (float64, error) {
						return 0.0, database.ErrNotFound
					},
				}

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   context.Background(),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Config{
							UptimePercentage: 0,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardValidatorTx{
									TxID: stakerTxID,
								},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; failed fetching net transformation",
			blkF: func(ctrl *gomock.Controller) *Block {
				var (
					stakerTxID = ids.GenerateTestID()
					nodeID     = ids.GenerateTestNodeID()
					netID   = ids.GenerateTestID()
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddPermissionlessValidatorTx{
							Validator: txs.Validator{
								NodeID: nodeID,
							},
							Net: netID,
						},
					}
					primaryNetworkValidatorStartTime = time.Now()
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
					}
				)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(stakerTx, status.Committed, nil)
				state.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, nodeID).Return(staker, nil)
				state.EXPECT().GetSubnetTransformation(netID).Return(nil, database.ErrNotFound)

				uptimes := &mockCalculator{}

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   context.Background(),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Config{
							UptimePercentage: 0,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardValidatorTx{
									TxID: stakerTxID,
								},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; prefers commit",
			blkF: func(ctrl *gomock.Controller) *Block {
				var (
					stakerTxID = ids.GenerateTestID()
					nodeID     = ids.GenerateTestNodeID()
					netID   = ids.GenerateTestID()
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddPermissionlessValidatorTx{
							Validator: txs.Validator{
								NodeID: nodeID,
							},
							Net: netID,
						},
					}
					primaryNetworkValidatorStartTime = time.Now()
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
					}
					transformSubnetTx = &txs.Tx{
						Unsigned: &txs.TransformNetTx{
							UptimeRequirement: .2 * reward.PercentDenominator,
						},
					}
				)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(stakerTx, status.Committed, nil)
				state.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, nodeID).Return(staker, nil)
				state.EXPECT().GetSubnetTransformation(netID).Return(transformSubnetTx, nil)

				uptimes := &mockCalculator{
					calculateUptimePercentFromF: func(ids.NodeID, ids.ID, time.Time) (float64, error) {
						return .5, nil
					},
				}

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   context.Background(),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Config{
							UptimePercentage: .8,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardValidatorTx{
									TxID: stakerTxID,
								},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffCommitBlock{},
		},
		{
			name: "banff proposal block; prefers abort",
			blkF: func(ctrl *gomock.Controller) *Block {
				var (
					stakerTxID = ids.GenerateTestID()
					nodeID     = ids.GenerateTestNodeID()
					netID   = ids.GenerateTestID()
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddPermissionlessValidatorTx{
							Validator: txs.Validator{
								NodeID: nodeID,
							},
							Net: netID,
						},
					}
					primaryNetworkValidatorStartTime = time.Now()
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
					}
					transformSubnetTx = &txs.Tx{
						Unsigned: &txs.TransformNetTx{
							UptimeRequirement: .6 * reward.PercentDenominator,
						},
					}
				)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(stakerTx, status.Committed, nil)
				state.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, nodeID).Return(staker, nil)
				state.EXPECT().GetSubnetTransformation(netID).Return(transformSubnetTx, nil)

				uptimes := &mockCalculator{
					calculateUptimePercentFromF: func(ids.NodeID, ids.ID, time.Time) (float64, error) {
						return .5, nil
					},
				}

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   context.Background(),
					},
					txExecutorBackend: &executor.Backend{
						Config: &config.Config{
							UptimePercentage: .8,
						},
						Uptimes: uptimes,
					},
				}

				return &Block{
					Block: &block.BanffProposalBlock{
						ApricotProposalBlock: block.ApricotProposalBlock{
							Tx: &txs.Tx{
								Unsigned: &txs.RewardValidatorTx{
									TxID: stakerTxID,
								},
							},
						},
					},
					manager: manager,
				}
			},
			expectedPreferenceType: &block.BanffAbortBlock{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			require := require.New(t)

			blk := tt.blkF(ctrl)
			options, err := blk.Options(context.Background())
			require.NoError(err)
			require.IsType(tt.expectedPreferenceType, options[0].(*Block).Block)
		})
	}
}
