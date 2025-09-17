// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	luxmetrics "github.com/luxfi/metric"

	"github.com/luxfi/node/utils/metric"
	"github.com/luxfi/node/utils/wrappers"
)

func newAverager(name string, reg luxmetrics.Registerer, errs *wrappers.Errs) metric.Averager {
	return metric.NewAveragerWithErrs(
		name,
		"time (in ns) of a "+name,
		reg,
		errs,
	)
}
