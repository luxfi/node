// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/luxfi/geth/core"
	"github.com/luxfi/geth/params"

	"github.com/luxfi/node/genesis"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/crypto/secp256k1"
	"github.com/luxfi/node/utils/formatting/address"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/platformvm/reward"
)

const (
	defaultGasLimit = uint64(100_000_000) // Gas limit is arbitrary

	// Arbitrarily large amount of LUX to fund keys on the X-Chain for testing
	defaultFundedKeyXChainAmount = 30 * units.MegaLux
)

var (
	// Arbitrarily large amount of LUX (10^12) to fund keys on the C-Chain for testing
	defaultFundedKeyCChainAmount = new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)

	errMissingStakersForGenesis = errors.New("no stakers provided for genesis")
)

// Create a genesis struct valid for bootstrapping a test
// network. Note that many of the genesis fields (e.g. reward
// addresses) are randomly generated or hard-coded.
func NewTestGenesisWithFunds(
	networkID uint32,
	nodes []*Node,
	keysToFund []*secp256k1.PrivateKey,
) (*genesis.UnparsedConfig, error) {
	// Validate inputs
	switch networkID {
	case constants.TestnetID, constants.MainnetID, constants.LocalID:
		return nil, errInvalidNetworkIDForGenesis
	}
	if len(nodes) == 0 {
		return nil, errMissingStakersForGenesis
	}
	if len(keysToFund) == 0 {
		return nil, errNoKeysForGenesis
	}

	initialStakers, err := stakersForNodes(networkID, nodes)
	if err != nil {
		return nil, fmt.Errorf("failed to configure stakers for nodes: %w", err)
	}

	// Address that controls stake doesn't matter -- generate it randomly
	stakeAddress, err := address.Format(
		"X",
		constants.GetHRP(networkID),
		ids.GenerateTestShortID().Bytes(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to format stake address: %w", err)
	}

	// Ensure the total stake allows a MegaLux per staker
	totalStake := uint64(len(initialStakers)) * units.MegaLux

	// The eth address is only needed to link pre-mainnet assets. Until that capability
	// becomes necessary for testing, use a bogus address.
	//
	// Reference: https://github.com/luxfi/node/issues/1365#issuecomment-1511508767
	ethAddress := "0x0000000000000000000000000000000000000000"

	now := time.Now()

	config := &genesis.UnparsedConfig{
		NetworkID: networkID,
		Allocations: []genesis.UnparsedAllocation{
			{
				ETHAddr:       ethAddress,
				LUXAddr:       stakeAddress,
				InitialAmount: 0,
				UnlockSchedule: []genesis.LockedAmount{ // Provides stake to validators
					{
						Amount:   totalStake,
						Locktime: uint64(now.Add(7 * 24 * time.Hour).Unix()), // 1 Week
					},
				},
			},
		},
		StartTime:                  uint64(now.Unix()),
		InitialStakedFunds:         []string{stakeAddress},
		InitialStakeDuration:       365 * 24 * 60 * 60, // 1 year
		InitialStakeDurationOffset: 90 * 60,            // 90 minutes
		Message:                    "hello lux!",
		InitialStakers:             initialStakers,
	}

	// Ensure pre-funded keys have arbitrary large balances on both chains to support testing
	xChainBalances := make(XChainBalanceMap, len(keysToFund))
	cChainBalances := make(core.GenesisAlloc, len(keysToFund))
	for _, key := range keysToFund {
		xChainBalances[key.Address()] = defaultFundedKeyXChainAmount
		cChainBalances[getEthAddress(key)] = core.GenesisAccount{
			Balance: defaultFundedKeyCChainAmount,
		}
	}

	// Set X-Chain balances
	for xChainAddress, balance := range xChainBalances {
		luxAddr, err := address.Format("X", constants.GetHRP(networkID), xChainAddress[:])
		if err != nil {
			return nil, fmt.Errorf("failed to format X-Chain address: %w", err)
		}
		config.Allocations = append(
			config.Allocations,
			genesis.UnparsedAllocation{
				ETHAddr:       ethAddress,
				LUXAddr:       luxAddr,
				InitialAmount: balance,
				UnlockSchedule: []genesis.LockedAmount{
					{
						Amount: 20 * units.MegaLux,
					},
					{
						Amount:   totalStake,
						Locktime: uint64(now.Add(7 * 24 * time.Hour).Unix()), // 1 Week
					},
				},
			},
		)
	}

	// Define C-Chain genesis
	cChainGenesis := &core.Genesis{
		Config:     params.LuxLocalChainConfig,
		Difficulty: big.NewInt(0), // Difficulty is a mandatory field
		GasLimit:   defaultGasLimit,
		Alloc:      cChainBalances,
	}
	cChainGenesisBytes, err := json.Marshal(cChainGenesis)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal C-Chain genesis: %w", err)
	}
	config.CChainGenesis = string(cChainGenesisBytes)

	return config, nil
}

// Returns staker configuration for the given set of nodes.
func stakersForNodes(networkID uint32, nodes []*Node) ([]genesis.UnparsedStaker, error) {
	// Give staking rewards for initial validators to a random address. Any testing of staking rewards
	// will be easier to perform with nodes other than the initial validators since the timing of
	// staking can be more easily controlled.
	rewardAddr, err := address.Format("X", constants.GetHRP(networkID), ids.GenerateTestShortID().Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to format reward address: %w", err)
	}

	// Configure provided nodes as initial stakers
	initialStakers := make([]genesis.UnparsedStaker, len(nodes))
	for i, node := range nodes {
		pop, err := node.GetProofOfPossession()
		if err != nil {
			return nil, fmt.Errorf("failed to derive proof of possession for node %s: %w", node.NodeID, err)
		}
		initialStakers[i] = genesis.UnparsedStaker{
			NodeID:        node.NodeID,
			RewardAddress: rewardAddr,
			DelegationFee: .01 * reward.PercentDenominator,
			Signer:        pop,
		}
	}

	return initialStakers, nil
}
