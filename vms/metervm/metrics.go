// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/node/v2/utils/metric"
	"github.com/luxfi/node/v2/utils/wrappers"
)

func newAverager(name string, reg prometheus.Registerer, errs *wrappers.Errs) metric.Averager {
	return metric.NewAveragerWithErrs(
		name,
		"time (in ns) of a "+name,
		reg,
		errs,
	)
}
