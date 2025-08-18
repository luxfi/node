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
	"github.com/luxfi/consensus/uptime/uptimemock"
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

				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   consensustest.Context(t, consensustest.PChainID),
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

				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   consensustest.Context(t, consensustest.PChainID),
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

				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   consensustest.Context(t, consensustest.PChainID),
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

				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   consensustest.Context(t, consensustest.PChainID),
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

				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   consensustest.Context(t, consensustest.PChainID),
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
					subnetID   = ids.GenerateTestID()
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddPermissionlessValidatorTx{
							Validator: txs.Validator{
								NodeID: nodeID,
							},
							Subnet: subnetID,
						},
					}
				)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(stakerTx, status.Committed, nil)
				state.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, nodeID).Return(nil, database.ErrNotFound)

				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   consensustest.Context(t, consensustest.PChainID),
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
					subnetID   = constants.PrimaryNetworkID
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddPermissionlessValidatorTx{
							Validator: txs.Validator{
								NodeID: nodeID,
							},
							Subnet: subnetID,
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

				uptimes := uptimemock.NewCalculator(ctrl)
				uptimes.EXPECT().CalculateUptimePercentFrom(gomock.Any(), gomock.Any()).Return(0.0, database.ErrNotFound)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   consensustest.Context(t, consensustest.PChainID),
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
			name: "banff proposal block; failed fetching subnet transformation",
			blkF: func(ctrl *gomock.Controller) *Block {
				var (
					stakerTxID = ids.GenerateTestID()
					nodeID     = ids.GenerateTestNodeID()
					subnetID   = ids.GenerateTestID()
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddPermissionlessValidatorTx{
							Validator: txs.Validator{
								NodeID: nodeID,
							},
							Subnet: subnetID,
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
				state.EXPECT().GetSubnetTransformation(subnetID).Return(nil, database.ErrNotFound)

				uptimes := uptimemock.NewCalculator(ctrl)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   consensustest.Context(t, consensustest.PChainID),
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
					subnetID   = ids.GenerateTestID()
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddPermissionlessValidatorTx{
							Validator: txs.Validator{
								NodeID: nodeID,
							},
							Subnet: subnetID,
						},
					}
					primaryNetworkValidatorStartTime = time.Now()
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
					}
					transformSubnetTx = &txs.Tx{
						Unsigned: &txs.TransformSubnetTx{
							UptimeRequirement: .2 * reward.PercentDenominator,
						},
					}
				)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(stakerTx, status.Committed, nil)
				state.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, nodeID).Return(staker, nil)
				state.EXPECT().GetSubnetTransformation(subnetID).Return(transformSubnetTx, nil)

				uptimes := uptimemock.NewCalculator(ctrl)
				uptimes.EXPECT().CalculateUptimePercentFrom(gomock.Any(), gomock.Any()).Return(.5, nil)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   consensustest.Context(t, consensustest.PChainID),
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
					subnetID   = ids.GenerateTestID()
					stakerTx   = &txs.Tx{
						Unsigned: &txs.AddPermissionlessValidatorTx{
							Validator: txs.Validator{
								NodeID: nodeID,
							},
							Subnet: subnetID,
						},
					}
					primaryNetworkValidatorStartTime = time.Now()
					staker                           = &state.Staker{
						StartTime: primaryNetworkValidatorStartTime,
					}
					transformSubnetTx = &txs.Tx{
						Unsigned: &txs.TransformSubnetTx{
							UptimeRequirement: .6 * reward.PercentDenominator,
						},
					}
				)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetTx(stakerTxID).Return(stakerTx, status.Committed, nil)
				state.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, nodeID).Return(staker, nil)
				state.EXPECT().GetSubnetTransformation(subnetID).Return(transformSubnetTx, nil)

				uptimes := uptimemock.NewCalculator(ctrl)
				uptimes.EXPECT().CalculateUptimePercentFrom(gomock.Any(), gomock.Any()).Return(.5, nil)

				manager := &manager{
					backend: &backend{
						state: state,
						ctx:   consensustest.Context(t, consensustest.PChainID),
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
