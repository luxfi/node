// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package upgradetest

import "time"

// TestConfig returns a test upgrade configuration
func TestConfig() map[string]time.Time {
	return map[string]time.Time{
		"testUpgrade": time.Now().Add(time.Hour),
	}
}