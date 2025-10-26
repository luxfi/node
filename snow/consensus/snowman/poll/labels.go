// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	terminationReason    = "reason"
	exhaustedReason      = "exhausted"
	earlyFailReason      = "early_fail"
)

var (
	errPollDurationVectorMetrics = errors.New("failed to register poll_duration vector metrics")
	errPollCountVectorMetrics    = errors.New("failed to register poll_count vector metrics")

	exhaustedLabel = prometheus.Labels{
		terminationReason: exhaustedReason,
	}
	earlyFailLabel = prometheus.Labels{
		terminationReason: earlyFailReason,
	}
)
