// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package upgrade publishes the canonical Lux genesis-activation timestamp
// and the runtime tunables consumed at chain birth. Every Lux chain runs the
// full feature-set from genesis (activate-all-implicitly); the fields here
// encode values, not gates.
package upgrade

import (
	"math"
	"time"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
)

// MerkleRootNeverActivate is the sentinel activation height meaning "never": no
// real block can reach math.MaxUint64, so a Config carrying this value keeps the
// xvm execution_root path OFF for the entire life of the chain. It is the
// default on Mainnet, Testnet, and Default until a concrete network upgrade
// picks a finite activation height. Below activation the xvm block path keeps
// the historical rule byte-for-byte (builder leaves the root empty; the executor
// rejects any non-empty root), so installing this sentinel is a no-op for live
// consensus.
const MerkleRootNeverActivate uint64 = math.MaxUint64

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
//   - MerkleRootActivationHeight is the xvm block height at and above which the
//     block's MerkleRoot must carry the xvm execution_root (computed over the
//     post-block state) instead of staying empty. It DEFAULTS to
//     MerkleRootNeverActivate (never) on every published network, so the
//     historical empty-root rule is preserved until a real upgrade sets a finite
//     height. It is a value (an activation point), not a feature flag.
type Config struct {
	XChainStopVertexID         ids.ID        `json:"xChainStopVertexID"`
	EpochDuration              time.Duration `json:"epochDuration"`
	MerkleRootActivationHeight uint64        `json:"merkleRootActivationHeight"`
}

// IsMerkleRootActivated reports whether the xvm execution_root must be stamped
// into (and verified on) a block at [height]. Below the activation height — and
// always, while the height is the never sentinel — it returns false and the
// historical empty-root rule applies.
func (c *Config) IsMerkleRootActivated(height uint64) bool {
	return height >= c.MerkleRootActivationHeight
}

// Validate is retained for callers that still invoke it; under activate-all-
// implicitly there is no upgrade ordering to enforce, so the function is a
// no-op.
func (*Config) Validate() error { return nil }

var (
	Mainnet = Config{
		XChainStopVertexID:         ids.FromStringOrPanic("jrGWDh5Po9FMj54depyunNixpia5PN4aAYxfmNzU8n752Rjga"),
		EpochDuration:              5 * time.Minute,
		MerkleRootActivationHeight: MerkleRootNeverActivate, // unset until a real upgrade picks a height
	}
	Testnet = Config{
		XChainStopVertexID:         ids.FromStringOrPanic("2D1cmbiG36BqQMRyHt4kFhWarmatA1ighSpND3FeFgz3vFVtCZ"),
		EpochDuration:              30 * time.Second,
		MerkleRootActivationHeight: MerkleRootNeverActivate, // unset until a real upgrade picks a height
	}
	Default = Config{
		XChainStopVertexID:         ids.Empty,
		EpochDuration:              30 * time.Second,
		MerkleRootActivationHeight: MerkleRootNeverActivate, // unset until a real upgrade picks a height
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
