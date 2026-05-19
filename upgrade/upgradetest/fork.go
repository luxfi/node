// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package upgradetest keeps the legacy Fork enum as an opaque test-config
// selector. Under activate-all-implicitly every fork value yields the same
// upgrade.Default config; the values are retained only so that the large
// surface of upstream-derived tests that pin behaviour to "fork X" keeps
// compiling.
package upgradetest

// Fork is an opaque selector. Its only consumer is GetConfig, which ignores it.
type Fork int

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
