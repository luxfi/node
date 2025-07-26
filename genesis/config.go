// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"cmp"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/formatting/address"
	"github.com/luxfi/node/utils/math"
	"github.com/luxfi/node/vms/platformvm/signer"
)

const localNetworkUpdateStartTimePeriod = 9 * 30 * 24 * time.Hour // 9 months

var (
	_ utils.Sortable[Allocation] = Allocation{}

	errInvalidGenesisJSON = errors.New("could not unmarshal genesis JSON")
)

type LockedAmount struct {
	Amount   uint64 `json:"amount"`
	Locktime uint64 `json:"locktime"`
}

type Allocation struct {
	ETHAddr        ids.ShortID    `json:"ethAddr"`
	LUXAddr       ids.ShortID    `json:"luxAddr"`
	InitialAmount  uint64         `json:"initialAmount"`
	UnlockSchedule []LockedAmount `json:"unlockSchedule"`
}

func (a Allocation) Unparse(networkID uint32) (UnparsedAllocation, error) {
	ua := UnparsedAllocation{
		InitialAmount:  a.InitialAmount,
		UnlockSchedule: a.UnlockSchedule,
		ETHAddr:        "0x" + hex.EncodeToString(a.ETHAddr.Bytes()),
	}
	luxAddr, err := address.Format(
		"X",
		constants.GetHRP(networkID),
		a.LUXAddr.Bytes(),
	)
	ua.LUXAddr = luxAddr
	return ua, err
}

func (a Allocation) Compare(other Allocation) int {
	if amountCmp := cmp.Compare(a.InitialAmount, other.InitialAmount); amountCmp != 0 {
		return amountCmp
	}
	return a.LUXAddr.Compare(other.LUXAddr)
}

type Staker struct {
	NodeID        ids.NodeID                `json:"nodeID"`
	RewardAddress ids.ShortID               `json:"rewardAddress"`
	DelegationFee uint32                    `json:"delegationFee"`
	Signer        *signer.ProofOfPossession `json:"signer,omitempty"`
}

func (s Staker) Unparse(networkID uint32) (UnparsedStaker, error) {
	luxAddr, err := address.Format(
		"X",
		constants.GetHRP(networkID),
		s.RewardAddress.Bytes(),
	)
	return UnparsedStaker{
		NodeID:        s.NodeID,
		RewardAddress: luxAddr,
		DelegationFee: s.DelegationFee,
		Signer:        s.Signer,
	}, err
}

// Config contains the genesis addresses used to construct a genesis
type Config struct {
	NetworkID uint32 `json:"networkID"`

	Allocations []Allocation `json:"allocations"`

	StartTime                  uint64        `json:"startTime"`
	InitialStakeDuration       uint64        `json:"initialStakeDuration"`
	InitialStakeDurationOffset uint64        `json:"initialStakeDurationOffset"`
	InitialStakedFunds         []ids.ShortID `json:"initialStakedFunds"`
	InitialStakers             []Staker      `json:"initialStakers"`

	CChainGenesis string `json:"cChainGenesis"`

	Message string `json:"message"`
}

func (c Config) Unparse() (UnparsedConfig, error) {
	uc := UnparsedConfig{
		NetworkID:                  c.NetworkID,
		Allocations:                make([]UnparsedAllocation, len(c.Allocations)),
		StartTime:                  c.StartTime,
		InitialStakeDuration:       c.InitialStakeDuration,
		InitialStakeDurationOffset: c.InitialStakeDurationOffset,
		InitialStakedFunds:         make([]string, len(c.InitialStakedFunds)),
		InitialStakers:             make([]UnparsedStaker, len(c.InitialStakers)),
		CChainGenesis:              c.CChainGenesis,
		Message:                    c.Message,
	}
	for i, a := range c.Allocations {
		ua, err := a.Unparse(uc.NetworkID)
		if err != nil {
			return uc, err
		}
		uc.Allocations[i] = ua
	}
	for i, isa := range c.InitialStakedFunds {
		luxAddr, err := address.Format(
			"X",
			constants.GetHRP(uc.NetworkID),
			isa.Bytes(),
		)
		if err != nil {
			return uc, err
		}
		uc.InitialStakedFunds[i] = luxAddr
	}
	for i, is := range c.InitialStakers {
		uis, err := is.Unparse(c.NetworkID)
		if err != nil {
			return uc, err
		}
		uc.InitialStakers[i] = uis
	}

	return uc, nil
}

func (c *Config) InitialSupply() (uint64, error) {
	initialSupply := uint64(0)
	for _, allocation := range c.Allocations {
		newInitialSupply, err := math.Add(initialSupply, allocation.InitialAmount)
		if err != nil {
			return 0, err
		}
		for _, unlock := range allocation.UnlockSchedule {
			newInitialSupply, err = math.Add(newInitialSupply, unlock.Amount)
			if err != nil {
				return 0, err
			}
		}
		initialSupply = newInitialSupply
	}
	return initialSupply, nil
}

var (
	// MainnetConfig is the config that should be used to generate the mainnet
	// genesis.
	MainnetConfig Config

	// TestnetConfig is the config that should be used to generate the testnet
	// genesis.
	TestnetConfig Config

	// LocalConfig is the config that should be used to generate a local
	// genesis.
	LocalConfig Config

	// LuxMainnetConfig is the config that should be used to generate the Lux
	// mainnet genesis with network ID 96369.
	LuxMainnetConfig Config

	// LuxTestnetConfig is the config that should be used to generate the Lux
	// testnet genesis with network ID 96368.
	LuxTestnetConfig Config

	// unmodifiedLocalConfig is the LocalConfig before advancing the StartTime
	// to a recent value.
	unmodifiedLocalConfig Config
)

func init() {
	unparsedMainnetConfig := UnparsedConfig{}
	unparsedTestnetConfig := UnparsedConfig{}
	unparsedLocalConfig := UnparsedConfig{}

	err := errors.Join(
		json.Unmarshal(mainnetGenesisConfigJSON, &unparsedMainnetConfig),
		json.Unmarshal(testnetGenesisConfigJSON, &unparsedTestnetConfig),
		json.Unmarshal(localGenesisConfigJSON, &unparsedLocalConfig),
	)
	if err != nil {
		panic(err)
	}
	MainnetConfig, err = unparsedMainnetConfig.Parse()
	if err != nil {
		panic(fmt.Sprintf("failed to parse mainnet config: %v", err))
	}

	TestnetConfig, err = unparsedTestnetConfig.Parse()
	if err != nil {
		panic(fmt.Sprintf("failed to parse testnet config: %v", err))
	}

	unmodifiedLocalConfig, err = unparsedLocalConfig.Parse()
	if err != nil {
		panic(fmt.Sprintf("failed to parse local config: %v", err))
	}

	// Renew the staking start time of the local config if required
	definedStartTime := time.Unix(int64(unmodifiedLocalConfig.StartTime), 0)
	recentStartTime := getRecentStartTime(
		definedStartTime,
		time.Now(),
		localNetworkUpdateStartTimePeriod,
	)

	LocalConfig = unmodifiedLocalConfig
	LocalConfig.StartTime = uint64(recentStartTime.Unix())

	// Initialize Lux mainnet config based on local config but with proper network ID
	// and chain ID. The actual genesis will be loaded from chain data.
	LuxMainnetConfig = LocalConfig
	LuxMainnetConfig.NetworkID = constants.LuxMainnetID
	
	// Update C-Chain genesis to use chain ID 96369
	if cChainGenesis, err := parseCChainGenesis([]byte(LocalConfig.CChainGenesis)); err == nil {
		if config, ok := cChainGenesis["config"].(map[string]interface{}); ok {
			config["chainId"] = float64(96369)
		}
		if cChainBytes, err := json.Marshal(cChainGenesis); err == nil {
			LuxMainnetConfig.CChainGenesis = string(cChainBytes)
		}
	}

	// Initialize Lux testnet config
	LuxTestnetConfig = LocalConfig
	LuxTestnetConfig.NetworkID = constants.LuxTestnetID
	
	// Update C-Chain genesis to use chain ID 96368
	if cChainGenesis, err := parseCChainGenesis([]byte(LocalConfig.CChainGenesis)); err == nil {
		if config, ok := cChainGenesis["config"].(map[string]interface{}); ok {
			config["chainId"] = float64(96368)
		}
		if cChainBytes, err := json.Marshal(cChainGenesis); err == nil {
			LuxTestnetConfig.CChainGenesis = string(cChainBytes)
		}
	}
}

// parseCChainGenesis parses C-Chain genesis JSON string into a map
func parseCChainGenesis(data []byte) (map[string]interface{}, error) {
	var genesis map[string]interface{}
	if err := json.Unmarshal(data, &genesis); err != nil {
		return nil, err
	}
	
	// Parse the config section
	if configData, ok := genesis["config"]; ok {
		if configMap, ok := configData.(map[string]interface{}); ok {
			genesis["config"] = configMap
		}
	}
	
	return genesis, nil
}

func GetConfig(networkID uint32) *Config {
	switch networkID {
	case constants.MainnetID:
		return &MainnetConfig
	case constants.TestnetID:
		return &TestnetConfig
	case constants.LocalID:
		return &LocalConfig
	case constants.LuxMainnetID:
		return &LuxMainnetConfig
	case constants.LuxTestnetID:
		return &LuxTestnetConfig
	default:
		tempConfig := LocalConfig
		tempConfig.NetworkID = networkID
		return &tempConfig
	}
}

// GetConfigFile loads a *Config from a provided filepath.
func GetConfigFile(fp string) (*Config, error) {
	bytes, err := os.ReadFile(filepath.Clean(fp))
	if err != nil {
		return nil, fmt.Errorf("unable to load file %s: %w", fp, err)
	}
	return parseGenesisJSONBytesToConfig(bytes)
}

// GetConfigContent loads a *Config from a provided environment variable
func GetConfigContent(genesisContent string) (*Config, error) {
	bytes, err := base64.StdEncoding.DecodeString(genesisContent)
	if err != nil {
		return nil, fmt.Errorf("unable to decode base64 content: %w", err)
	}
	return parseGenesisJSONBytesToConfig(bytes)
}

func parseGenesisJSONBytesToConfig(bytes []byte) (*Config, error) {
	var unparsedConfig UnparsedConfig
	if err := json.Unmarshal(bytes, &unparsedConfig); err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidGenesisJSON, err)
	}

	config, err := unparsedConfig.Parse()
	if err != nil {
		return nil, fmt.Errorf("unable to parse config: %w", err)
	}
	return &config, nil
}

// getRecentStartTime advances [definedStartTime] in chunks of [period]. It
// returns the latest startTime that isn't after [now].
func getRecentStartTime(
	definedStartTime time.Time,
	now time.Time,
	period time.Duration,
) time.Time {
	startTime := definedStartTime
	for {
		nextStartTime := startTime.Add(period)
		if now.Before(nextStartTime) {
			break
		}
		startTime = nextStartTime
	}
	return startTime
}
