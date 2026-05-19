// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package upgrade publishes the canonical Lux genesis-activation timestamp and
// the small set of runtime tunables that survived the upstream upgrade rip.
//
// All historical "fork timestamp + IsXxxActivated()" gates were deleted under
// the activate-all-implicitly directive: every Lux chain runs the post-Granite
// rule-set from genesis, so the legacy predicates are unreachable. The fields
// retained here encode real runtime values, not gates.
package upgrade

import (
	"time"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
)

// InitiallyActiveTime is the canonical Lux genesis-activation timestamp. It is
// referenced by code paths that historically demanded "post-fork timestamp" as
// an input and now want a single deterministic value.
var InitiallyActiveTime = time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC)

// Config carries the runtime tunables that survived the upstream upgrade rip.
//
//   - CortinaXChainStopVertexID encodes the per-network stop-vertex ID that
//     pins X-Chain genesis state at boot. It is a value, not a gate.
//   - GraniteEpochDuration is the LP-181 epoch duration; per-network value
//     (5m on mainnet, 30s on test/dev) tunes consensus pacing.
type Config struct {
	CortinaXChainStopVertexID ids.ID        `json:"cortinaXChainStopVertexID"`
	GraniteEpochDuration      time.Duration `json:"graniteEpochDuration"`
}

// Validate is retained for callers that still invoke it; under activate-all-
// implicitly there is no upgrade ordering to enforce, so the function is a
// no-op.
func (*Config) Validate() error { return nil }

var (
	Mainnet = Config{
		CortinaXChainStopVertexID: ids.FromStringOrPanic("jrGWDh5Po9FMj54depyunNixpia5PN4aAYxfmNzU8n752Rjga"),
		GraniteEpochDuration:      5 * time.Minute,
	}
	Testnet = Config{
		CortinaXChainStopVertexID: ids.FromStringOrPanic("2D1cmbiG36BqQMRyHt4kFhWarmatA1ighSpND3FeFgz3vFVtCZ"),
		GraniteEpochDuration:      30 * time.Second,
	}
	Default = Config{
		CortinaXChainStopVertexID: ids.Empty,
		GraniteEpochDuration:      30 * time.Second,
	}
)

// GetConfig resolves the per-network runtime config.
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

// AlwaysOn is the activate-all-implicitly bridge that satisfies
// runtime.NetworkUpgrades for callers that haven't yet been migrated off the
// legacy predicate interface. Every method returns true.
type AlwaysOn struct{}

func (AlwaysOn) IsApricotPhase1Activated(time.Time) bool     { return true }
func (AlwaysOn) IsApricotPhase2Activated(time.Time) bool     { return true }
func (AlwaysOn) IsApricotPhase3Activated(time.Time) bool     { return true }
func (AlwaysOn) IsApricotPhase4Activated(time.Time) bool     { return true }
func (AlwaysOn) IsApricotPhase5Activated(time.Time) bool     { return true }
func (AlwaysOn) IsApricotPhasePre6Activated(time.Time) bool  { return true }
func (AlwaysOn) IsApricotPhase6Activated(time.Time) bool     { return true }
func (AlwaysOn) IsApricotPhasePost6Activated(time.Time) bool { return true }
func (AlwaysOn) IsBanffActivated(time.Time) bool             { return true }
func (AlwaysOn) IsCortinaActivated(time.Time) bool           { return true }
func (AlwaysOn) IsDurangoActivated(time.Time) bool           { return true }
func (AlwaysOn) IsEtnaActivated(time.Time) bool              { return true }
func (AlwaysOn) IsFortunaActivated(time.Time) bool           { return true }
func (AlwaysOn) IsGraniteActivated(time.Time) bool           { return true }
