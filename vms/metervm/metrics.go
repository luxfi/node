// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/luxfi/metric"

	utilmetric "github.com/luxfi/node/utils/metric"
	"github.com/luxfi/node/utils/wrappers"
)

func newAverager(name string, reg metric.Registerer, errs *wrappers.Errs) utilmetric.Averager {
	return utilmetric.NewAveragerWithErrs(
		name,
		"time (in ns) of a "+name,
		reg,
		errs,
	)
}
