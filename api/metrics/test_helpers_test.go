// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"context"

	dto "github.com/prometheus/client_model/go"
)

type testGatherer struct {
	mfs []*dto.MetricFamily
}

func (g *testGatherer) Gather(context.Context) ([]*dto.MetricFamily, error) {
	return g.mfs, nil
}
