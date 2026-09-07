// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidDefaultUpgrades(t *testing.T) {
	for _, c := range []struct {
		name    string
		upgrade Config
	}{
		{name: "Default", upgrade: Default},
		{name: "Testnet", upgrade: Testnet},
		{name: "Mainnet", upgrade: Mainnet},
	} {
		t.Run(c.name, func(t *testing.T) {
			require.NoError(t, c.upgrade.Validate())
		})
	}
}
