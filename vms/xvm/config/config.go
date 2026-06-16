// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

// Struct collecting all the foundational parameters of the XVM
type Config struct {
	// Fee that is burned by every non-asset creating transaction
	TxFee uint64 `json:"txFee"`

	// Fee that must be burned by every asset creating transaction
	CreateAssetTxFee uint64 `json:"createAssetTxFee"`

	// IndexTransactions enables transaction indexing by address
	IndexTransactions bool `json:"indexTransactions"`

	// MerkleRootActivationHeight is the xvm block height at and above which the
	// block's MerkleRoot must carry the xvm execution_root over the post-block
	// state. It is sourced from upgrade.Config at VM init and defaults to
	// upgrade.MerkleRootNeverActivate (math.MaxUint64) on every published
	// network, so the historical empty-root rule holds until a real upgrade
	// picks a finite height. A zero value (the Go struct zero) activates from
	// genesis and is used only by tests that exercise the activated path.
	MerkleRootActivationHeight uint64 `json:"merkleRootActivationHeight"`
}

// IsMerkleRootActivated reports whether the xvm execution_root must be stamped
// into (and verified on) a block at [height]. Mirrors
// upgrade.Config.IsMerkleRootActivated so the block path can gate on the value
// carried in the backend without reaching back into the upgrade package.
//
// It is nil-safe: a nil config (a backend constructed without one) is OFF, so
// the gate fails safe to the historical empty-root rule. Production always
// carries a config whose height is sourced from upgrade.Config — the never
// sentinel by default — so the activated path engages only when an operator
// deliberately sets a finite height.
func (c *Config) IsMerkleRootActivated(height uint64) bool {
	if c == nil {
		return false
	}
	return height >= c.MerkleRootActivationHeight
}
