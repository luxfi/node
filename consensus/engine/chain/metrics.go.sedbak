// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"github.com/luxfi/metrics"
	"github.com/luxfi/node/utils/wrappers"
)

const (
	pullGossipSource = "pull_gossip"
	pushGossipSource = "push_gossip"
	builtSource      = "built"
	unknownSource    = "unknown"
)

type chainMetrics struct {
	bootstrapFinished                     metrics.Gauge
	numRequests                           metrics.Gauge
	numBlocked                            metrics.Gauge
	numBlockers                           metrics.Gauge
	numNonVerifieds                       metrics.Gauge
	numBuilt                              metrics.Counter
	numBuildsFailed                       metrics.Counter
	numUselessPutBytes                    metrics.Counter
	numUselessPushQueryBytes              metrics.Counter
	numMissingAcceptedBlocks              metrics.Counter
	numProcessingAncestorFetchesFailed    metrics.Counter
	numProcessingAncestorFetchesDropped   metrics.Counter
	numProcessingAncestorFetchesSucceeded metrics.Counter
	numProcessingAncestorFetchesUnneeded  metrics.Counter
	selectedVoteIndex                     Averager
	issuerStake                           Averager
	issued                                metrics.CounterVec
	blockTimeSkew                         metrics.Gauge
}

func newMetrics(reg metrics.Registry) (*chainMetrics, error) {
	errs := wrappers.Errs{}
	// Create a metrics instance
	metricsInstance := metrics.NewWithRegistry("", reg)
	
	m := &chainMetrics{
		bootstrapFinished: metricsInstance.NewGauge(
			"bootstrap_finished",
			"Whether or not bootstrap process has completed. 1 is success, 0 is fail or ongoing.",
		),
		numRequests: metricsInstance.NewGauge(
			"requests",
			"Number of outstanding block requests",
		),
		numBlocked: metricsInstance.NewGauge(
			"blocked",
			"Number of blocks that are pending issuance",
		),
		numBlockers: metricsInstance.NewGauge(
			"blockers",
			"Number of blocks that are blocking other blocks from being issued because they haven't been issued",
		),
		numNonVerifieds: metricsInstance.NewGauge(
			"non_verified_blks",
			"Number of non-verified blocks in the memory",
		),
		numBuilt: metricsInstance.NewCounter(
			"blks_built",
			"Number of blocks that have been built locally",
		),
		numBuildsFailed: metricsInstance.NewCounter(
			"blk_builds_failed",
			"Number of BuildBlock calls that have failed",
		),
		numUselessPutBytes: metricsInstance.NewCounter(
			"num_useless_put_bytes",
			"Amount of useless bytes received in Put messages",
		),
		numUselessPushQueryBytes: metricsInstance.NewCounter(
			"num_useless_push_query_bytes",
			"Amount of useless bytes received in PushQuery messages",
		),
		numMissingAcceptedBlocks: metricsInstance.NewCounter(
			"num_missing_accepted_blocks",
			"Number of times an accepted block height was referenced and it wasn't locally available",
		),
		numProcessingAncestorFetchesFailed: metricsInstance.NewCounter(
			"num_processing_ancestor_fetches_failed",
			"Number of votes that were dropped due to unknown blocks",
		),
		numProcessingAncestorFetchesDropped: metricsInstance.NewCounter(
			"num_processing_ancestor_fetches_dropped",
			"Number of votes that were dropped due to decided blocks",
		),
		numProcessingAncestorFetchesSucceeded: metricsInstance.NewCounter(
			"num_processing_ancestor_fetches_succeeded",
			"Number of votes that were applied to ancestor blocks",
		),
		numProcessingAncestorFetchesUnneeded: metricsInstance.NewCounter(
			"num_processing_ancestor_fetches_unneeded",
			"Number of votes that were directly applied to blocks",
		),
		selectedVoteIndex: NewAveragerWithErrs(
			"selected_vote_index",
			"index of the voteID that was passed into consensus",
			reg,
			&errs,
		),
		issuerStake: NewAveragerWithErrs(
			"issuer_stake",
			"stake weight of the peer who provided a block that was issued into consensus",
			reg,
			&errs,
		),
		issued: metricsInstance.NewCounterVec(
			"blks_issued",
			"number of blocks that have been issued into consensus by discovery mechanism",
			[]string{"source"},
		),
		blockTimeSkew: metricsInstance.NewGauge(
			"blks_built_time_skew",
			"The differences between the time the block was built at and the block's timestamp",
		),
	}

	// Register the labels
	m.issued.WithLabelValues(pullGossipSource)
	m.issued.WithLabelValues(pushGossipSource)
	m.issued.WithLabelValues(builtSource)
	m.issued.WithLabelValues(unknownSource)

	// No need to manually register with luxfi/metrics - they're auto-registered
	return m, errs.Err
}
