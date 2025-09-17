// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package resource

import (
	"github.com/prometheus/client_golang/prometheus"
	"errors"

	"github.com/luxfi/metric"
)

type metricsImpl struct {
	numCPUCycles       metric.GaugeVec
	numDiskReads       metric.GaugeVec
	numDiskReadBytes   metric.GaugeVec
	numDiskWrites      metric.GaugeVec
	numDiskWritesBytes metric.GaugeVec
}

func newMetrics(registerer metric.Registerer) (*metricsImpl, error) {
	m := &metricsImpl{
		numCPUCycles: metric.NewGaugeVec(
			metric.GaugeOpts{
				Name: "num_cpu_cycles",
				Help: "Total number of CPU cycles",
			},
			[]string{"processID"},
		),
		numDiskReads: metric.NewGaugeVec(
			metric.GaugeOpts{
				Name: "num_disk_reads",
				Help: "Total number of disk reads",
			},
			[]string{"processID"},
		),
		numDiskReadBytes: metric.NewGaugeVec(
			metric.GaugeOpts{
				Name: "num_disk_read_bytes",
				Help: "Total number of disk read bytes",
			},
			[]string{"processID"},
		),
		numDiskWrites: metric.NewGaugeVec(
			metric.GaugeOpts{
				Name: "num_disk_writes",
				Help: "Total number of disk writes",
			},
			[]string{"processID"},
		),
		numDiskWritesBytes: metric.NewGaugeVec(
			metric.GaugeOpts{
				Name: "num_disk_write_bytes",
				Help: "Total number of disk write bytes",
			},
			[]string{"processID"},
		),
	}
	err := errors.Join(
		registerer.Register(m.numCPUCycles.(prometheus.Collector)),
		registerer.Register(m.numDiskReads.(prometheus.Collector)),
		registerer.Register(m.numDiskReadBytes.(prometheus.Collector)),
		registerer.Register(m.numDiskWrites.(prometheus.Collector)),
		registerer.Register(m.numDiskWritesBytes.(prometheus.Collector)),
	)
	return m, err
}
