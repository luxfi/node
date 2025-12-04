// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/ids"
	"github.com/luxfi/genesis/pkg/genesis"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/formatting/address"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/platformvm/reward"
)

var (
	ErrInvalidNetworkID = errors.New("invalid network ID: cannot be 0")
	ErrNoValidators     = errors.New("at least one validator is required")
)

// GenesisConfig defines configuration for creating a genesis block
type GenesisConfig struct {
	NetworkID         uint32
	ChainID           *big.Int
	Timestamp         uint64
	GasLimit          uint64
	BaseFee           *big.Int
	PreFundedAccounts map[common.Address]*big.Int
	Validators        []ValidatorConfig
}

// ValidatorConfig defines a validator for genesis
type ValidatorConfig struct {
	NodeID ids.NodeID
	Weight uint64
}

// GenesisBlock represents a genesis block with essential fields
type GenesisBlock struct {
	Number       uint64
	Hash         common.Hash
	ParentHash   common.Hash
	StateRoot    common.Hash
	Timestamp    uint64
	GasLimit     uint64
	BaseFee      *big.Int
	Header       []byte
	Transactions []byte
}

// DefaultGenesisConfig returns a default genesis configuration for testing
func DefaultGenesisConfig() GenesisConfig {
	return GenesisConfig{
		NetworkID: 88888,
		ChainID:   big.NewInt(88888),
		Timestamp: uint64(time.Now().Unix()),
		GasLimit:  100_000_000,
		BaseFee:   big.NewInt(25_000_000_000),
		PreFundedAccounts: map[common.Address]*big.Int{
			common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"): new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1000)),
		},
	}
}

// CreateGenesisBlock creates a genesis block from configuration
func CreateGenesisBlock(cfg GenesisConfig) (*GenesisBlock, error) {
	if cfg.NetworkID == 0 {
		return nil, ErrInvalidNetworkID
	}

	// Calculate state root from pre-funded accounts
	stateRoot := CalculateStateRoot(cfg.PreFundedAccounts)

	// Generate genesis hash
	genesisHash := CalculateGenesisHash(cfg)

	return &GenesisBlock{
		Number:     0,
		Hash:       genesisHash,
		ParentHash: common.Hash{},
		StateRoot:  stateRoot,
		Timestamp:  cfg.Timestamp,
		GasLimit:   cfg.GasLimit,
		BaseFee:    cfg.BaseFee,
	}, nil
}

// CalculateStateRoot computes a deterministic state root from accounts
func CalculateStateRoot(accounts map[common.Address]*big.Int) common.Hash {
	if len(accounts) == 0 {
		return common.Hash{}
	}

	// Simplified state root calculation for testing
	// In production, this would build a merkle patricia trie
	hash := common.Hash{}
	for addr, balance := range accounts {
		for i := 0; i < 20 && i < len(addr); i++ {
			hash[i] ^= addr[i]
		}
		balanceBytes := balance.Bytes()
		for i := 0; i < len(balanceBytes) && i < 12; i++ {
			hash[20+i] ^= balanceBytes[i]
		}
	}
	return hash
}

// CalculateGenesisHash computes the genesis block hash
func CalculateGenesisHash(cfg GenesisConfig) common.Hash {
	// Simplified hash calculation for testing
	hash := common.Hash{}
	hash[0] = byte(cfg.NetworkID >> 24)
	hash[1] = byte(cfg.NetworkID >> 16)
	hash[2] = byte(cfg.NetworkID >> 8)
	hash[3] = byte(cfg.NetworkID)
	hash[4] = byte(cfg.Timestamp >> 56)
	hash[5] = byte(cfg.Timestamp >> 48)
	hash[6] = byte(cfg.Timestamp >> 40)
	hash[7] = byte(cfg.Timestamp >> 32)
	return hash
}

// MultiChainGenesisConfig defines configuration for multi-chain genesis
type MultiChainGenesisConfig struct {
	NetworkID  uint32
	Timestamp  uint64
	Validators []ValidatorConfig
	Allocations []AllocationConfig
}

// AllocationConfig defines an allocation in genesis
type AllocationConfig struct {
	Address common.Address
	Balance *big.Int
}

// CreateMultiChainGenesis creates a full network genesis with P, X, and C chains
func CreateMultiChainGenesis(cfg MultiChainGenesisConfig) (*genesis.UnparsedConfig, error) {
	if cfg.NetworkID == 0 {
		return nil, ErrInvalidNetworkID
	}

	now := time.Now()
	if cfg.Timestamp == 0 {
		cfg.Timestamp = uint64(now.Unix())
	}

	// Create stake address
	stakeAddress, err := address.Format(
		"X",
		constants.GetHRP(cfg.NetworkID),
		ids.GenerateTestShortID().Bytes(),
	)
	if err != nil {
		return nil, err
	}

	// Calculate total stake
	totalStake := uint64(len(cfg.Validators)) * units.MegaLux
	if totalStake == 0 {
		totalStake = units.MegaLux
	}

	config := &genesis.UnparsedConfig{
		NetworkID: cfg.NetworkID,
		Allocations: []genesis.UnparsedAllocation{
			{
				ETHAddr:       "0x0000000000000000000000000000000000000000",
				LUXAddr:       stakeAddress,
				InitialAmount: 0,
				UnlockSchedule: []genesis.LockedAmount{
					{
						Amount:   totalStake,
						Locktime: uint64(now.Add(7 * 24 * time.Hour).Unix()),
					},
				},
			},
		},
		StartTime:                  cfg.Timestamp,
		InitialStakedFunds:         []string{stakeAddress},
		InitialStakeDuration:       365 * 24 * 60 * 60,
		InitialStakeDurationOffset: 90 * 60,
		Message:                    "lux regenesis test",
	}

	// Add validators
	if len(cfg.Validators) > 0 {
		rewardAddr, err := address.Format("X", constants.GetHRP(cfg.NetworkID), ids.GenerateTestShortID().Bytes())
		if err != nil {
			return nil, err
		}

		config.InitialStakers = make([]genesis.UnparsedStaker, len(cfg.Validators))
		for i, v := range cfg.Validators {
			config.InitialStakers[i] = genesis.UnparsedStaker{
				NodeID:        v.NodeID,
				RewardAddress: rewardAddr,
				DelegationFee: .01 * reward.PercentDenominator,
			}
		}
	}

	// Add C-Chain genesis
	cChainGenesis := map[string]interface{}{
		"config": map[string]interface{}{
			"chainId": cfg.NetworkID,
		},
		"difficulty": "0x0",
		"timestamp":  cfg.Timestamp,
		"gasLimit":   "0x5F5E100", // 100M
		"alloc":      map[string]interface{}{},
	}

	cChainGenesisBytes, err := json.Marshal(cChainGenesis)
	if err != nil {
		return nil, err
	}
	config.CChainGenesis = string(cChainGenesisBytes)

	return config, nil
}

// TestChainConfig represents a minimal chain config for testing
type TestChainConfig struct {
	ChainID *big.Int `json:"chainId"`
}

// TestGenesisAccount represents a genesis account for testing
type TestGenesisAccount struct {
	Balance *big.Int `json:"balance"`
}

// TestGenesis represents a minimal genesis for testing
type TestGenesis struct {
	Config     *TestChainConfig                      `json:"config"`
	Timestamp  uint64                                `json:"timestamp,omitempty"`
	GasLimit   uint64                                `json:"gasLimit"`
	Difficulty *big.Int                              `json:"difficulty"`
	Alloc      map[common.Address]TestGenesisAccount `json:"alloc"`
}

// CreateTestGenesis creates a test genesis with the given configuration
func CreateTestGenesis(networkID uint32, accounts map[common.Address]*big.Int) *TestGenesis {
	alloc := make(map[common.Address]TestGenesisAccount)
	for addr, balance := range accounts {
		alloc[addr] = TestGenesisAccount{Balance: balance}
	}

	return &TestGenesis{
		Config:     &TestChainConfig{ChainID: big.NewInt(int64(networkID))},
		Timestamp:  uint64(time.Now().Unix()),
		GasLimit:   100_000_000,
		Difficulty: big.NewInt(0),
		Alloc:      alloc,
	}
}
