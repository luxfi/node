// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgradetest

import (
	"time"

	"github.com/luxfi/node/upgrade"
)

// GetConfig returns the activate-all-implicitly upgrade config used by tests.
// The fork enum is ignored: every upgrade is live from genesis.
func GetConfig(Fork) upgrade.Config {
	return upgrade.Default
}

// GetConfigWithUpgradeTime is preserved for tests that still pass an upgrade
// time; the time is ignored under activate-all-implicitly.
func GetConfigWithUpgradeTime(Fork, time.Time) upgrade.Config {
	return upgrade.Default
}

// SetTimesTo is preserved as a no-op for tests that still call it.
func SetTimesTo(*upgrade.Config, Fork, time.Time) {}
