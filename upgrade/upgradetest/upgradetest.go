// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgradetest

import "github.com/luxfi/node/upgrade"

// LatestVersion is a stable token identifying the activate-all-implicitly
// upgrade snapshot used by tests.
const LatestVersion = "latest"

// GetConfigForVersion returns the canonical upgrade config; under activate-
// all-implicitly all version tokens map to the same Lux default.
func GetConfigForVersion(string) upgrade.Config {
	return upgrade.Default
}
