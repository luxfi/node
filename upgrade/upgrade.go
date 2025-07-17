// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import "time"

// Config contains the upgrade configuration
type Config struct {
	// Time when the upgrade should take effect
	ActivationTime time.Time
}