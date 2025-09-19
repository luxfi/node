// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"testing"
)

// TestBeaconManager_DataRace tests for data races in beacon manager
// TODO: Reimplement with proper mocking framework
func TestBeaconManager_DataRace(t *testing.T) {
	t.Skip("Test needs refactoring for new ChainRouter interface")
}
