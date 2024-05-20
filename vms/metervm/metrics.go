// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/node/utils/metric"
	"github.com/luxfi/node/utils/wrappers"
)

func newAverager(namespace, name string, reg prometheus.Registerer, errs *wrappers.Errs) metric.Averager {
	return metric.NewAveragerWithErrs(
		namespace,
		name,
		fmt.Sprintf("time (in ns) of a %s", name),
		reg,
		errs,
	)
}
