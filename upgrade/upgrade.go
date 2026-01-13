// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import (
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
)

var (
	InitiallyActiveTime       = time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC)
	UnscheduledActivationTime = time.Date(9999, time.December, 1, 0, 0, 0, 0, time.UTC)

	Mainnet = Config{
		ApricotPhase1Time:            InitiallyActiveTime,
		ApricotPhase2Time:            InitiallyActiveTime,
		ApricotPhase3Time:            InitiallyActiveTime,
		ApricotPhase4Time:            InitiallyActiveTime,
		ApricotPhase4MinPChainHeight: 0,
		ApricotPhase5Time:            InitiallyActiveTime,
		ApricotPhasePre6Time:         InitiallyActiveTime,
		ApricotPhase6Time:            InitiallyActiveTime,
		ApricotPhasePost6Time:        InitiallyActiveTime,
		BanffTime:                    time.Date(2022, time.October, 18, 16, 0, 0, 0, time.UTC),
		CortinaTime:                  time.Date(2023, time.April, 25, 15, 0, 0, 0, time.UTC),
		// The mainnet stop vertex is well known. It can be verified on any
		// fully synced node by looking at the parentID of the genesis block.
		//
		// Ref: https://chains.lux.network/x-chain/block/0
		CortinaXChainStopVertexID: ids.FromStringOrPanic("jrGWDh5Po9FMj54depyunNixpia5PN4aAYxfmNzU8n752Rjga"),
		DurangoTime:               time.Date(2025, time.March, 6, 16, 0, 0, 0, time.UTC),
		EtnaTime:                  time.Date(2025, time.April, 8, 15, 0, 0, 0, time.UTC),
		FortunaTime:               time.Date(2025, time.December, 16, 17, 0, 0, 0, time.UTC),
		GraniteTime:               UnscheduledActivationTime,
		GraniteEpochDuration:      5 * time.Minute,
	}
	Testnet = Config{
		ApricotPhase1Time:            InitiallyActiveTime,
		ApricotPhase2Time:            InitiallyActiveTime,
		ApricotPhase3Time:            InitiallyActiveTime,
		ApricotPhase4Time:            InitiallyActiveTime,
		ApricotPhase4MinPChainHeight: 0,
		ApricotPhase5Time:            InitiallyActiveTime,
		ApricotPhasePre6Time:         InitiallyActiveTime,
		ApricotPhase6Time:            InitiallyActiveTime,
		ApricotPhasePost6Time:        InitiallyActiveTime,
		BanffTime:                    time.Date(2022, time.October, 3, 14, 0, 0, 0, time.UTC),
		CortinaTime:                  time.Date(2023, time.April, 6, 15, 0, 0, 0, time.UTC),
		// The testnet stop vertex is well known. It can be verified on any fully
		// synced node by looking at the parentID of the genesis block.
		//
		// Ref: https://chains-test.lux.network/x-chain/block/0
		CortinaXChainStopVertexID: ids.FromStringOrPanic("2D1cmbiG36BqQMRyHt4kFhWarmatA1ighSpND3FeFgz3vFVtCZ"),
		DurangoTime:               time.Date(2025, time.February, 13, 16, 0, 0, 0, time.UTC),
		EtnaTime:                  time.Date(2025, time.March, 13, 15, 0, 0, 0, time.UTC),
		FortunaTime:               time.Date(2025, time.November, 25, 16, 0, 0, 0, time.UTC),
		GraniteTime:               UnscheduledActivationTime,
		GraniteEpochDuration:      30 * time.Second, // Faster epochs for testnet
	}
	Default = Config{
		ApricotPhase1Time:            InitiallyActiveTime,
		ApricotPhase2Time:            InitiallyActiveTime,
		ApricotPhase3Time:            InitiallyActiveTime,
		ApricotPhase4Time:            InitiallyActiveTime,
		ApricotPhase4MinPChainHeight: 0,
		ApricotPhase5Time:            InitiallyActiveTime,
		ApricotPhasePre6Time:         InitiallyActiveTime,
		ApricotPhase6Time:            InitiallyActiveTime,
		ApricotPhasePost6Time:        InitiallyActiveTime,
		BanffTime:                    InitiallyActiveTime,
		CortinaTime:                  InitiallyActiveTime,
		CortinaXChainStopVertexID:    ids.Empty,
		DurangoTime:                  InitiallyActiveTime,
		EtnaTime:                     InitiallyActiveTime,
		FortunaTime:                  InitiallyActiveTime,
		GraniteTime:                  UnscheduledActivationTime,
		GraniteEpochDuration:         30 * time.Second,
	}

	ErrInvalidUpgradeTimes = errors.New("invalid upgrade configuration")
)

type Config struct {
	ApricotPhase1Time            time.Time     `json:"apricotPhase1Time"`
	ApricotPhase2Time            time.Time     `json:"apricotPhase2Time"`
	ApricotPhase3Time            time.Time     `json:"apricotPhase3Time"`
	ApricotPhase4Time            time.Time     `json:"apricotPhase4Time"`
	ApricotPhase4MinPChainHeight uint64        `json:"apricotPhase4MinPChainHeight"`
	ApricotPhase5Time            time.Time     `json:"apricotPhase5Time"`
	ApricotPhasePre6Time         time.Time     `json:"apricotPhasePre6Time"`
	ApricotPhase6Time            time.Time     `json:"apricotPhase6Time"`
	ApricotPhasePost6Time        time.Time     `json:"apricotPhasePost6Time"`
	BanffTime                    time.Time     `json:"banffTime"`
	CortinaTime                  time.Time     `json:"cortinaTime"`
	CortinaXChainStopVertexID    ids.ID        `json:"cortinaXChainStopVertexID"`
	DurangoTime                  time.Time     `json:"durangoTime"`
	EtnaTime                     time.Time     `json:"etnaTime"`
	FortunaTime                  time.Time     `json:"fortunaTime"`
	GraniteTime                  time.Time     `json:"graniteTime"`
	GraniteEpochDuration         time.Duration `json:"graniteEpochDuration"`
}

func (c *Config) Validate() error {
	upgrades := []time.Time{
		c.ApricotPhase1Time,
		c.ApricotPhase2Time,
		c.ApricotPhase3Time,
		c.ApricotPhase4Time,
		c.ApricotPhase5Time,
		c.ApricotPhasePre6Time,
		c.ApricotPhase6Time,
		c.ApricotPhasePost6Time,
		c.BanffTime,
		c.CortinaTime,
		c.DurangoTime,
		c.EtnaTime,
		c.FortunaTime,
		c.GraniteTime,
	}
	for i := 0; i < len(upgrades)-1; i++ {
		if upgrades[i].After(upgrades[i+1]) {
			return fmt.Errorf("%w: upgrade %d (%s) is after upgrade %d (%s)",
				ErrInvalidUpgradeTimes,
				i,
				upgrades[i],
				i+1,
				upgrades[i+1],
			)
		}
	}
	return nil
}

func (c *Config) IsApricotPhase1Activated(t time.Time) bool {
	return !t.Before(c.ApricotPhase1Time)
}

func (c *Config) IsApricotPhase2Activated(t time.Time) bool {
	return !t.Before(c.ApricotPhase2Time)
}

func (c *Config) IsApricotPhase3Activated(t time.Time) bool {
	return !t.Before(c.ApricotPhase3Time)
}

func (c *Config) IsApricotPhase4Activated(t time.Time) bool {
	return !t.Before(c.ApricotPhase4Time)
}

func (c *Config) IsApricotPhase5Activated(t time.Time) bool {
	return !t.Before(c.ApricotPhase5Time)
}

func (c *Config) IsApricotPhasePre6Activated(t time.Time) bool {
	return !t.Before(c.ApricotPhasePre6Time)
}

func (c *Config) IsApricotPhase6Activated(t time.Time) bool {
	return !t.Before(c.ApricotPhase6Time)
}

func (c *Config) IsApricotPhasePost6Activated(t time.Time) bool {
	return !t.Before(c.ApricotPhasePost6Time)
}

func (c *Config) IsBanffActivated(t time.Time) bool {
	return !t.Before(c.BanffTime)
}

func (c *Config) IsCortinaActivated(t time.Time) bool {
	return !t.Before(c.CortinaTime)
}

func (c *Config) IsDurangoActivated(t time.Time) bool {
	return !t.Before(c.DurangoTime)
}

func (c *Config) IsEtnaActivated(t time.Time) bool {
	return !t.Before(c.EtnaTime)
}

func (c *Config) IsFortunaActivated(t time.Time) bool {
	return !t.Before(c.FortunaTime)
}

func (c *Config) IsGraniteActivated(t time.Time) bool {
	return !t.Before(c.GraniteTime)
}

func GetConfig(networkID uint32) Config {
	switch networkID {
	case constants.MainnetID:
		return Mainnet
	case constants.TestnetID:
		return Testnet
	default:
		return Default
	}
}
