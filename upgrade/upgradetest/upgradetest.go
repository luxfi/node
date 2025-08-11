// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgradetest

import (
	"time"
	
	
	"github.com/luxfi/node/upgrade"
)

// TestConfig returns a test upgrade configuration
func TestConfig() map[string]time.Time {
	return map[string]time.Time{
		"testUpgrade": time.Now().Add(time.Hour),
	}
}

// Latest represents the latest upgrade configuration for testing
const Latest = "latest"

// GetConfig returns an upgrade configuration for testing
func GetConfig(version string) upgrade.Config {
	return upgrade.Config{
		ActivationTime: time.Now().Add(time.Hour),
	}
}
