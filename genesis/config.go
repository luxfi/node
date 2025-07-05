// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
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

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/formatting/address"
	"github.com/luxfi/node/utils/math"
	"github.com/luxfi/node/vms/platformvm/signer"
)

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
	ValidatorNFT  *ValidatorNFT             `json:"validatorNFT,omitempty"`
}

type ValidatorNFT struct {
	ContractAddress string `json:"contractAddress"`
	TokenID         uint64 `json:"tokenId"`
	CollectionName  string `json:"collectionName"`
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
	AChainGenesis string `json:"aChainGenesis,omitempty"`
	BChainGenesis string `json:"bChainGenesis,omitempty"`
	ZChainGenesis string `json:"zChainGenesis,omitempty"`

	NFTStakingConfig *NFTStakingConfig `json:"nftStakingConfig,omitempty"`
	RingtailConfig   *RingtailConfig   `json:"ringtailConfig,omitempty"`
	MPCConfig        *MPCConfig        `json:"mpcConfig,omitempty"`

	Message string `json:"message"`
}

type NFTStakingConfig struct {
	Enabled         bool             `json:"enabled"`
	NFTContract     string           `json:"nftContract"`
	RequiredBalance uint64           `json:"requiredBalance"`
	ValidatorTiers  []ValidatorTier  `json:"validatorTiers"`
}

type ValidatorTier struct {
	Name              string `json:"name"`
	MinTokenID        uint64 `json:"minTokenId"`
	MaxTokenID        uint64 `json:"maxTokenId"`
	StakingMultiplier uint32 `json:"stakingMultiplier"`
}

type RingtailConfig struct {
	Enabled           bool   `json:"enabled"`
	SignatureVersion  string `json:"signatureVersion"`
	RingSize          uint32 `json:"ringSize"`
	PublicParameters  string `json:"publicParameters"`
}

type MPCConfig struct {
	Enabled              bool     `json:"enabled"`
	Threshold            uint32   `json:"threshold"`
	Parties              uint32   `json:"parties"`
	PerAccountMPC        bool     `json:"perAccountMPC"`
	DefaultKeyGenProtocol string   `json:"defaultKeyGenProtocol"`
	SupportedProtocols   []string `json:"supportedProtocols"`
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
		newInitialSupply, err := math.Add64(initialSupply, allocation.InitialAmount)
		if err != nil {
			return 0, err
		}
		for _, unlock := range allocation.UnlockSchedule {
			newInitialSupply, err = math.Add64(newInitialSupply, unlock.Amount)
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

	// FujiConfig is the config that should be used to generate the fuji
	// genesis.
	FujiConfig Config

	// LocalConfig is the config that should be used to generate a local
	// genesis.
	LocalConfig Config

	// unmodifiedLocalConfig is the LocalConfig before advancing the StartTime
	// to a recent value.
	unmodifiedLocalConfig Config
)

func init() {
	unparsedMainnetConfig := UnparsedConfig{}
	unparsedFujiConfig := UnparsedConfig{}
	unparsedLocalConfig := UnparsedConfig{}

	err := errors.Join(
		json.Unmarshal(mainnetGenesisConfigJSON, &unparsedMainnetConfig),
		json.Unmarshal(fujiGenesisConfigJSON, &unparsedFujiConfig),
		json.Unmarshal(localGenesisConfigJSON, &unparsedLocalConfig),
	)
	if err != nil {
		panic(err)
	}

	MainnetConfig, err = unparsedMainnetConfig.Parse()
	if err != nil {
		panic(err)
	}

	FujiConfig, err = unparsedFujiConfig.Parse()
	if err != nil {
		panic(err)
	}

	LocalConfig, err = unparsedLocalConfig.Parse()
	if err != nil {
		panic(err)
	}

	FujiConfig, err = unparsedFujiConfig.Parse()
	if err != nil {
		panic(err)
	}

	unmodifiedLocalConfig, err = unparsedLocalConfig.Parse()
	if err != nil {
		panic(err)
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
}

func GetConfig(networkID uint32) *Config {
	switch networkID {
	case constants.MainnetID:
		return &MainnetConfig
	case constants.FujiID:
		return &FujiConfig
	case constants.LocalID:
		return &LocalConfig
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
