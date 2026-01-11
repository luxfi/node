// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package factory

import (
	"reflect"
	"strings"
	"testing"

	"github.com/luxfi/metric"
)

func requireMetricsEnabled(t *testing.T) {
	t.Helper()

	reg := metric.NewRegistry()
	if reg == nil {
		t.Skip("metrics registry is nil")
	}

	if strings.Contains(reflect.TypeOf(reg).String(), "noopRegistry") {
		t.Skip("metrics disabled in this build")
	}
}
