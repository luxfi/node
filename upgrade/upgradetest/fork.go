// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package upgradetest exposes a Fork enum identifying every legacy upstream
// network upgrade. The enum values are retained verbatim so that the large
// surface of upstream-derived test code that pins behaviour to "fork X" keeps
// compiling. In production Lux all forks are active from InitiallyActiveTime
// (Dec 5, 2020); the Fork enum has no policy effect outside test fixtures.
package upgradetest

const (
	NoUpgrades Fork = iota
	ApricotPhase1
	ApricotPhase2
	ApricotPhase3
	ApricotPhase4
	ApricotPhase5
	ApricotPhasePre6
	ApricotPhase6
	ApricotPhasePost6
	Banff
	Cortina
	Durango
	Etna
	Fortuna
	Granite

	Latest = Granite
)

// Fork is an enum of all the major network upgrades.
type Fork int

func (f Fork) String() string {
	switch f {
	case Granite:
		return "Granite"
	case Fortuna:
		return "Fortuna"
	case Etna:
		return "Etna"
	case Durango:
		return "Durango"
	case Cortina:
		return "Cortina"
	case Banff:
		return "Banff"
	case ApricotPhasePost6:
		return "ApricotPhasePost6"
	case ApricotPhase6:
		return "ApricotPhase6"
	case ApricotPhasePre6:
		return "ApricotPhasePre6"
	case ApricotPhase5:
		return "ApricotPhase5"
	case ApricotPhase4:
		return "ApricotPhase4"
	case ApricotPhase3:
		return "ApricotPhase3"
	case ApricotPhase2:
		return "ApricotPhase2"
	case ApricotPhase1:
		return "ApricotPhase1"
	case NoUpgrades:
		return "NoUpgrades"
	default:
		return "Unknown"
	}
}
