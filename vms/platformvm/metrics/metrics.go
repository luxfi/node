// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"time"

	"github.com/luxfi/metric"

	"github.com/luxfi/ids"
	utilmetric "github.com/luxfi/node/utils/metric"
	"github.com/luxfi/node/utils/wrappers"
	"github.com/luxfi/node/vms/platformvm/block"
)

var _ Metrics = (*metricsImpl)(nil)

type Metrics interface {
	utilmetric.APIInterceptor

	// Mark that the given block was accepted.
	MarkAccepted(block.Block) error
	// Mark that a validator set was created.
	IncValidatorSetsCreated()
	// Mark that a validator set was cached.
	IncValidatorSetsCached()
	// Mark that we spent the given time computing validator diffs.
	AddValidatorSetsDuration(time.Duration)
	// Mark that we computed a validator diff at a height with the given
	// difference from the top.
	AddValidatorSetsHeightDiff(uint64)
	// Mark that this much stake is staked on the node.
	SetLocalStake(uint64)
	// Mark that this much stake is staked in the network.
	SetTotalStake(uint64)
	// Mark when this node will unstake from the Primary Network.
	SetTimeUntilUnstake(time.Duration)
	// Mark when this node will unstake from a subnet.
	SetTimeUntilSubnetUnstake(netID ids.ID, timeUntilUnstake time.Duration)
}

func New(registerer metric.Registerer) (Metrics, error) {
	blockMetrics, err := newBlockMetrics(registerer)
	m := &metricsImpl{
		blockMetrics: blockMetrics,
		timeUntilUnstake: metric.NewGauge(metric.GaugeOpts{
			Name: "time_until_unstake",
			Help: "Time (in ns) until this node leaves the Primary Network's validator set",
		}),
		timeUntilSubnetUnstake: metric.NewGaugeVec(
			metric.GaugeOpts{
				Name: "time_until_unstake_subnet",
				Help: "Time (in ns) until this node leaves the subnet's validator set",
			},
			[]string{"netID"},
		),
		localStake: metric.NewGauge(metric.GaugeOpts{
			Name: "local_staked",
			Help: "Amount (in nLUX) of LUX staked on this node",
		}),
		totalStake: metric.NewGauge(metric.GaugeOpts{
			Name: "total_staked",
			Help: "Amount (in nLUX) of LUX staked on the Primary Network",
		}),

		validatorSetsCached: metric.NewCounter(metric.CounterOpts{
			Name: "validator_sets_cached",
			Help: "Total number of validator sets cached",
		}),
		validatorSetsCreated: metric.NewCounter(metric.CounterOpts{
			Name: "validator_sets_created",
			Help: "Total number of validator sets created from applying difflayers",
		}),
		validatorSetsHeightDiff: metric.NewGauge(metric.GaugeOpts{
			Name: "validator_sets_height_diff_sum",
			Help: "Total number of validator sets diffs applied for generating validator sets",
		}),
		validatorSetsDuration: metric.NewGauge(metric.GaugeOpts{
			Name: "validator_sets_duration_sum",
			Help: "Total amount of time generating validator sets in nanoseconds",
		}),
	}

	errs := wrappers.Errs{Err: err}
	apiRequestMetrics, err := utilmetric.NewAPIInterceptor(registerer)
	errs.Add(err)
	m.APIInterceptor = apiRequestMetrics

	// Metrics created with NewCounter, NewGauge etc. need to be manually registered
	// but since they don't directly expose prometheus.Collector interface, we need
	// a different approach - just return any errors from creating metrics

	return m, errs.Err
}

type metricsImpl struct {
	utilmetric.APIInterceptor

	blockMetrics *blockMetrics

	timeUntilUnstake       metric.Gauge
	timeUntilSubnetUnstake metric.GaugeVec
	localStake             metric.Gauge
	totalStake             metric.Gauge

	validatorSetsCached     metric.Counter
	validatorSetsCreated    metric.Counter
	validatorSetsHeightDiff metric.Gauge
	validatorSetsDuration   metric.Gauge
}

func (m *metricsImpl) MarkAccepted(b block.Block) error {
	return b.Visit(m.blockMetrics)
}

func (m *metricsImpl) IncValidatorSetsCreated() {
	m.validatorSetsCreated.Inc()
}

func (m *metricsImpl) IncValidatorSetsCached() {
	m.validatorSetsCached.Inc()
}

func (m *metricsImpl) AddValidatorSetsDuration(d time.Duration) {
	m.validatorSetsDuration.Add(float64(d))
}

func (m *metricsImpl) AddValidatorSetsHeightDiff(d uint64) {
	m.validatorSetsHeightDiff.Add(float64(d))
}

func (m *metricsImpl) SetLocalStake(s uint64) {
	m.localStake.Set(float64(s))
}

func (m *metricsImpl) SetTotalStake(s uint64) {
	m.totalStake.Set(float64(s))
}

func (m *metricsImpl) SetTimeUntilUnstake(timeUntilUnstake time.Duration) {
	m.timeUntilUnstake.Set(float64(timeUntilUnstake))
}

func (m *metricsImpl) SetTimeUntilSubnetUnstake(netID ids.ID, timeUntilUnstake time.Duration) {
	m.timeUntilSubnetUnstake.WithLabelValues(netID.String()).Set(float64(timeUntilUnstake))
}
