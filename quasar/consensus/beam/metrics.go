// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package beam

import (
	"github.com/luxfi/metrics"
)

// beamMetrics tracks consensus metrics
type beamMetrics struct {
	// Block metrics
	blocksProposed  metrics.Counter
	blocksAccepted  metrics.Counter
	blocksRejected  metrics.Counter

	// Certificate metrics
	blsCertTime     metrics.Histogram
	rtCertTime      metrics.Histogram
	dualCertTime    metrics.Histogram

	// Quantum metrics
	quantumFinality metrics.Counter
	slashingEvents  metrics.Counter
	quasarTimeouts  metrics.Counter

	// Poll metrics
	pollsStarted    metrics.Counter
	pollsFinished   metrics.Counter
	pollDuration    metrics.Histogram

	// Performance metrics
	blockBuildTime  metrics.Histogram
	verifyTime      metrics.Histogram
	consensusTime   metrics.Histogram
}

// newMetrics creates new metrics
func newMetrics(m metrics.Metrics) *beamMetrics {
	return &beamMetrics{
		blocksProposed: m.NewCounter("quasar_beam_blocks_proposed", "Number of blocks proposed"),
		blocksAccepted: m.NewCounter("quasar_beam_blocks_accepted", "Number of blocks accepted"),
		blocksRejected: m.NewCounter("quasar_beam_blocks_rejected", "Number of blocks rejected"),

		blsCertTime: m.NewHistogram("quasar_beam_bls_cert_time", "Time to create BLS certificate", 
			[]float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1}),
		rtCertTime: m.NewHistogram("quasar_beam_rt_cert_time", "Time to create Ringtail certificate",
			[]float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}),
		dualCertTime: m.NewHistogram("quasar_beam_dual_cert_time", "Time to create dual certificates",
			[]float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}),

		quantumFinality: m.NewCounter("quasar_beam_quantum_finality", "Blocks finalized with quantum security"),
		slashingEvents:  m.NewCounter("quasar_beam_slashing_events", "Number of slashing events"),
		quasarTimeouts:  m.NewCounter("quasar_beam_quasar_timeouts", "Number of Quasar timeouts"),

		pollsStarted:  m.NewCounter("quasar_beam_polls_started", "Number of polls started"),
		pollsFinished: m.NewCounter("quasar_beam_polls_finished", "Number of polls finished"),
		pollDuration: m.NewHistogram("quasar_beam_poll_duration", "Duration of consensus polls",
			[]float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}),

		blockBuildTime: m.NewHistogram("quasar_beam_block_build_time", "Time to build blocks",
			[]float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1}),
		verifyTime: m.NewHistogram("quasar_beam_verify_time", "Time to verify blocks",
			[]float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5}),
		consensusTime: m.NewHistogram("quasar_beam_consensus_time", "Time to reach consensus",
			[]float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 25, 50}),
	}
}