// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package servicenodevm

import (
	"errors"
	"time"
)

// Default configuration values
const (
	DefaultMinStake          = uint64(1000000)       // 1 LUX in microLUX
	DefaultStakeLockPeriod   = 7 * 24 * time.Hour    // 7 days
	DefaultEpochDuration     = 24 * time.Hour        // 1 day
	DefaultEpochBlocks       = uint64(43200)         // ~24h at 2s blocks
	DefaultNodesPerSwarm     = uint32(5)
	DefaultMinActiveNodes    = uint32(10)
	DefaultChallengeInterval = 1 * time.Hour
	DefaultChallengeTTL      = 5 * time.Minute
	DefaultMessageTTL        = 14 * 24 * time.Hour // 14 days
	DefaultMaxMessageSize    = uint64(64 * 1024)   // 64KB
	DefaultStorageQuota      = uint64(100 * 1024 * 1024) // 100MB per account
	DefaultJailDuration      = 24 * time.Hour
	DefaultSlashPercent      = uint64(1000)        // 10% in basis points
	DefaultRewardPerMessage  = uint64(100)         // microLUX per message
	DefaultRewardPerChallenge = uint64(10)         // microLUX per challenge passed
	DefaultMaxMessagesPerFetch = 100
)

// Config holds the ServiceNodeVM configuration
type Config struct {
	// Staking parameters
	MinStake        uint64 `json:"minStake"`
	StakeLockPeriod int64  `json:"stakeLockPeriod"` // Seconds

	// Epoch parameters
	EpochDuration    int64  `json:"epochDuration"`    // Seconds
	EpochBlocks      uint64 `json:"epochBlocks"`
	NodesPerSwarm    uint32 `json:"nodesPerSwarm"`
	MinActiveNodes   uint32 `json:"minActiveNodes"`

	// Challenge parameters
	ChallengeInterval int64 `json:"challengeInterval"` // Seconds
	ChallengeTTL      int64 `json:"challengeTTL"`      // Seconds
	ChallengesPerEpoch uint32 `json:"challengesPerEpoch"`

	// Storage parameters
	MessageTTL        int64  `json:"messageTTL"`        // Seconds
	MaxMessageSize    uint64 `json:"maxMessageSize"`
	StorageQuota      uint64 `json:"storageQuota"`      // Per account
	MaxMessagesPerFetch int  `json:"maxMessagesPerFetch"`

	// Penalty parameters
	JailDuration  int64  `json:"jailDuration"`  // Seconds
	SlashPercent  uint64 `json:"slashPercent"`  // Basis points (100 = 1%)
	MaxFailedChallenges uint32 `json:"maxFailedChallenges"`

	// Reward parameters
	RewardPerMessage   uint64 `json:"rewardPerMessage"`
	RewardPerChallenge uint64 `json:"rewardPerChallenge"`
	EpochRewardPool    uint64 `json:"epochRewardPool"`

	// Network parameters
	EnableOnionRouting bool `json:"enableOnionRouting"`
	EnableQUIC         bool `json:"enableQUIC"`
	MaxPeers           int  `json:"maxPeers"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		MinStake:           DefaultMinStake,
		StakeLockPeriod:    int64(DefaultStakeLockPeriod.Seconds()),
		EpochDuration:      int64(DefaultEpochDuration.Seconds()),
		EpochBlocks:        DefaultEpochBlocks,
		NodesPerSwarm:      DefaultNodesPerSwarm,
		MinActiveNodes:     DefaultMinActiveNodes,
		ChallengeInterval:  int64(DefaultChallengeInterval.Seconds()),
		ChallengeTTL:       int64(DefaultChallengeTTL.Seconds()),
		ChallengesPerEpoch: 24,
		MessageTTL:         int64(DefaultMessageTTL.Seconds()),
		MaxMessageSize:     DefaultMaxMessageSize,
		StorageQuota:       DefaultStorageQuota,
		MaxMessagesPerFetch: DefaultMaxMessagesPerFetch,
		JailDuration:       int64(DefaultJailDuration.Seconds()),
		SlashPercent:       DefaultSlashPercent,
		MaxFailedChallenges: 3,
		RewardPerMessage:   DefaultRewardPerMessage,
		RewardPerChallenge: DefaultRewardPerChallenge,
		EpochRewardPool:    1000000000, // 1000 LUX
		EnableOnionRouting: true,
		EnableQUIC:         true,
		MaxPeers:           100,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.MinStake == 0 {
		return errors.New("minStake must be > 0")
	}
	if c.NodesPerSwarm < 3 {
		return errors.New("nodesPerSwarm must be >= 3 for redundancy")
	}
	if c.EpochBlocks == 0 {
		return errors.New("epochBlocks must be > 0")
	}
	if c.MaxMessageSize == 0 {
		return errors.New("maxMessageSize must be > 0")
	}
	if c.SlashPercent > 10000 {
		return errors.New("slashPercent must be <= 10000 (100%)")
	}
	return nil
}

// Merge merges another config into this one, using non-zero values
func (c *Config) Merge(other *Config) {
	if other == nil {
		return
	}
	if other.MinStake > 0 {
		c.MinStake = other.MinStake
	}
	if other.StakeLockPeriod > 0 {
		c.StakeLockPeriod = other.StakeLockPeriod
	}
	if other.EpochDuration > 0 {
		c.EpochDuration = other.EpochDuration
	}
	if other.EpochBlocks > 0 {
		c.EpochBlocks = other.EpochBlocks
	}
	if other.NodesPerSwarm > 0 {
		c.NodesPerSwarm = other.NodesPerSwarm
	}
	if other.MinActiveNodes > 0 {
		c.MinActiveNodes = other.MinActiveNodes
	}
	if other.ChallengeInterval > 0 {
		c.ChallengeInterval = other.ChallengeInterval
	}
	if other.ChallengeTTL > 0 {
		c.ChallengeTTL = other.ChallengeTTL
	}
	if other.ChallengesPerEpoch > 0 {
		c.ChallengesPerEpoch = other.ChallengesPerEpoch
	}
	if other.MessageTTL > 0 {
		c.MessageTTL = other.MessageTTL
	}
	if other.MaxMessageSize > 0 {
		c.MaxMessageSize = other.MaxMessageSize
	}
	if other.StorageQuota > 0 {
		c.StorageQuota = other.StorageQuota
	}
	if other.MaxMessagesPerFetch > 0 {
		c.MaxMessagesPerFetch = other.MaxMessagesPerFetch
	}
	if other.JailDuration > 0 {
		c.JailDuration = other.JailDuration
	}
	if other.SlashPercent > 0 {
		c.SlashPercent = other.SlashPercent
	}
	if other.MaxFailedChallenges > 0 {
		c.MaxFailedChallenges = other.MaxFailedChallenges
	}
	if other.RewardPerMessage > 0 {
		c.RewardPerMessage = other.RewardPerMessage
	}
	if other.RewardPerChallenge > 0 {
		c.RewardPerChallenge = other.RewardPerChallenge
	}
	if other.EpochRewardPool > 0 {
		c.EpochRewardPool = other.EpochRewardPool
	}
	// Booleans are merged only if explicitly true
	if other.EnableOnionRouting {
		c.EnableOnionRouting = true
	}
	if other.EnableQUIC {
		c.EnableQUIC = true
	}
	if other.MaxPeers > 0 {
		c.MaxPeers = other.MaxPeers
	}
}
