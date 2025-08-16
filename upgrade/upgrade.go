// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import (
	"errors"
	"time"
)

var (
	// InitiallyActiveTime is the time that upgrades are initially active
	InitiallyActiveTime = time.Unix(0, 0)

	// UnscheduledActivationTime is the time that upgrades are scheduled to be inactive
	UnscheduledActivationTime = time.Unix(1<<63-1, 0)

	// ErrInvalidUpgradeTimes is returned when upgrade times are invalid
	ErrInvalidUpgradeTimes = errors.New("invalid upgrade configuration")
)

// Config contains the upgrade configuration
type Config struct {
	// Time when the upgrade should take effect
	ActivationTime time.Time

	// Individual upgrade times
	ApricotPhase1Time     time.Time
	ApricotPhase2Time     time.Time
	ApricotPhase3Time     time.Time
	ApricotPhase4Time     time.Time
	ApricotPhase5Time     time.Time
	ApricotPhasePre6Time  time.Time
	ApricotPhase6Time     time.Time
	ApricotPhasePost6Time time.Time
	BanffTime             time.Time
	CortinaTime           time.Time
	DurangoTime           time.Time
	EtnaTime              time.Time
	FortunaTime           time.Time
	GraniteTime           time.Time
}

// Default upgrade configuration with all upgrades initially active
var Default = Config{
	ApricotPhase1Time:     InitiallyActiveTime,
	ApricotPhase2Time:     InitiallyActiveTime,
	ApricotPhase3Time:     InitiallyActiveTime,
	ApricotPhase4Time:     InitiallyActiveTime,
	ApricotPhase5Time:     InitiallyActiveTime,
	ApricotPhasePre6Time:  InitiallyActiveTime,
	ApricotPhase6Time:     InitiallyActiveTime,
	ApricotPhasePost6Time: InitiallyActiveTime,
	BanffTime:             InitiallyActiveTime,
	CortinaTime:           InitiallyActiveTime,
	DurangoTime:           InitiallyActiveTime,
	EtnaTime:              UnscheduledActivationTime,
	FortunaTime:           UnscheduledActivationTime,
	GraniteTime:           UnscheduledActivationTime,
}

// Fuji testnet upgrade configuration
var Fuji = Config{
	ApricotPhase1Time:     InitiallyActiveTime,
	ApricotPhase2Time:     InitiallyActiveTime,
	ApricotPhase3Time:     InitiallyActiveTime,
	ApricotPhase4Time:     InitiallyActiveTime,
	ApricotPhase5Time:     InitiallyActiveTime,
	ApricotPhasePre6Time:  InitiallyActiveTime,
	ApricotPhase6Time:     InitiallyActiveTime,
	ApricotPhasePost6Time: InitiallyActiveTime,
	BanffTime:             InitiallyActiveTime,
	CortinaTime:           InitiallyActiveTime,
	DurangoTime:           InitiallyActiveTime,
	EtnaTime:              UnscheduledActivationTime,
	FortunaTime:           UnscheduledActivationTime,
	GraniteTime:           UnscheduledActivationTime,
}

// Mainnet upgrade configuration
var Mainnet = Config{
	ApricotPhase1Time:     InitiallyActiveTime,
	ApricotPhase2Time:     InitiallyActiveTime,
	ApricotPhase3Time:     InitiallyActiveTime,
	ApricotPhase4Time:     InitiallyActiveTime,
	ApricotPhase5Time:     InitiallyActiveTime,
	ApricotPhasePre6Time:  InitiallyActiveTime,
	ApricotPhase6Time:     InitiallyActiveTime,
	ApricotPhasePost6Time: InitiallyActiveTime,
	BanffTime:             InitiallyActiveTime,
	CortinaTime:           InitiallyActiveTime,
	DurangoTime:           InitiallyActiveTime,
	EtnaTime:              UnscheduledActivationTime,
	FortunaTime:           UnscheduledActivationTime,
	GraniteTime:           UnscheduledActivationTime,
}
