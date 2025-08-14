// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import "time"

var (
	// InitiallyActiveTime is the time that upgrades are initially active
	InitiallyActiveTime = time.Unix(0, 0)
	
	// UnscheduledActivationTime is the time that upgrades are scheduled to be inactive
	UnscheduledActivationTime = time.Unix(1<<63-1, 0)
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
