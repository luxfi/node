// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package upgrade publishes the canonical Lux genesis-activation timestamp
// and the runtime tunables consumed at chain birth. Every Lux chain runs the
// full feature-set from genesis (activate-all-implicitly); the fields here
// encode values, not gates.
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
//   - XChainStopVertexID encodes the per-network stop-vertex ID that pins
//     X-Chain genesis state at boot. It is a value, not a gate.
//   - EpochDuration is the LP-181 epoch duration; per-network value
//     (5m on mainnet, 30s on test/dev) tunes consensus pacing.
type Config struct {
	XChainStopVertexID ids.ID        `json:"xChainStopVertexID"`
	EpochDuration      time.Duration `json:"epochDuration"`
}

// Validate is retained for callers that still invoke it; under activate-all-
// implicitly there is no upgrade ordering to enforce, so the function is a
// no-op.
func (*Config) Validate() error { return nil }

var (
	Mainnet = Config{
		XChainStopVertexID: ids.FromStringOrPanic("jrGWDh5Po9FMj54depyunNixpia5PN4aAYxfmNzU8n752Rjga"),
		EpochDuration:      5 * time.Minute,
	}
	Testnet = Config{
		XChainStopVertexID: ids.FromStringOrPanic("2D1cmbiG36BqQMRyHt4kFhWarmatA1ighSpND3FeFgz3vFVtCZ"),
		EpochDuration:      30 * time.Second,
	}
	Default = Config{
		XChainStopVertexID: ids.Empty,
		EpochDuration:      30 * time.Second,
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
