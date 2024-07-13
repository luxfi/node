// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"

	"github.com/luxfi/node/ids"
)

var NoOpCalculator Calculator = noOpCalculator{}

type noOpCalculator struct{}

func (noOpCalculator) CalculateUptime(ids.NodeID, ids.ID) (time.Duration, time.Time, error) {
	return 0, time.Time{}, nil
}

func (noOpCalculator) CalculateUptimePercent(ids.NodeID, ids.ID) (float64, error) {
	return 0, nil
}

func (noOpCalculator) CalculateUptimePercentFrom(ids.NodeID, ids.ID, time.Time) (float64, error) {
	return 0, nil
}
