// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"errors"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/luxfi/metric"
)

type Metrics struct {
	txsIssuedCounter    metric.Counter
	txsConfirmedCounter metric.Counter
	txsFailedCounter    metric.Counter
	txLatency           metric.Histogram
	
	// Extended metrics for percentile tracking
	latencyTracker *LatencyTracker
	tpsGauge       metric.Gauge
	successRate    metric.Gauge
}

func NewMetrics(registry metric.Registry) (*Metrics, error) {
	m := &Metrics{
		txsIssuedCounter: metric.NewCounter(metric.CounterOpts{
			Namespace: namespace,
			Name:      "txs_issued",
			Help:      "Number of transactions issued",
		}),
		txsConfirmedCounter: metric.NewCounter(metric.CounterOpts{
			Namespace: namespace,
			Name:      "txs_confirmed",
			Help:      "Number of transactions confirmed",
		}),
		txsFailedCounter: metric.NewCounter(metric.CounterOpts{
			Namespace: namespace,
			Name:      "txs_failed",
			Help:      "Number of transactions failed",
		}),
		txLatency: metric.NewHistogram(metric.HistogramOpts{
			Namespace: namespace,
			Name:      "tx_latency",
			Help:      "Latency of transactions in milliseconds",
			Buckets:   []float64{10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000},
		}),
		tpsGauge: metric.NewGauge(metric.GaugeOpts{
			Namespace: namespace,
			Name:      "tps",
			Help:      "Current transactions per second",
		}),
		successRate: metric.NewGauge(metric.GaugeOpts{
			Namespace: namespace,
			Name:      "success_rate",
			Help:      "Transaction success rate (0-1)",
		}),
		latencyTracker: NewLatencyTracker(),
	}

	if err := errors.Join(
		registry.Register(m.txsIssuedCounter),
		registry.Register(m.txsConfirmedCounter),
		registry.Register(m.txsFailedCounter),
		registry.Register(m.txLatency),
		registry.Register(m.tpsGauge),
		registry.Register(m.successRate),
	); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Metrics) IncIssuedTx() {
	m.txsIssuedCounter.Inc()
}

func (m *Metrics) RecordConfirmedTx(latencyMS float64) {
	m.txsConfirmedCounter.Inc()
	m.txLatency.Observe(latencyMS)
	m.latencyTracker.Record(latencyMS)
}

func (m *Metrics) RecordFailedTx(latencyMS float64) {
	m.txsFailedCounter.Inc()
	m.txLatency.Observe(latencyMS)
	m.latencyTracker.Record(latencyMS)
}

func (m *Metrics) UpdateTPS(tps float64) {
	m.tpsGauge.Set(tps)
}

func (m *Metrics) UpdateSuccessRate(rate float64) {
	m.successRate.Set(rate)
}

func (m *Metrics) GetLatencyPercentiles() LatencyPercentiles {
	return m.latencyTracker.GetPercentiles()
}

// LatencyTracker tracks latency samples for percentile calculation
type LatencyTracker struct {
	mu       sync.RWMutex
	samples  []float64
	maxSize  int
	window   time.Duration
	lastClean time.Time
}

func NewLatencyTracker() *LatencyTracker {
	return &LatencyTracker{
		samples:   make([]float64, 0, 10000),
		maxSize:   10000, // Keep last 10k samples
		window:    5 * time.Minute,
		lastClean: time.Now(),
	}
}

func (lt *LatencyTracker) Record(latencyMS float64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	lt.samples = append(lt.samples, latencyMS)

	// Maintain a sliding window of samples
	if len(lt.samples) > lt.maxSize {
		// Keep the most recent samples
		copy(lt.samples, lt.samples[len(lt.samples)-lt.maxSize:])
		lt.samples = lt.samples[:lt.maxSize]
	}
}

func (lt *LatencyTracker) GetPercentiles() LatencyPercentiles {
	lt.mu.RLock()
	defer lt.mu.RUnlock()

	if len(lt.samples) == 0 {
		return LatencyPercentiles{}
	}

	// Create a copy to avoid modifying original
	sorted := make([]float64, len(lt.samples))
	copy(sorted, lt.samples)
	sort.Float64s(sorted)

	return LatencyPercentiles{
		P50:     percentile(sorted, 0.50),
		P75:     percentile(sorted, 0.75),
		P90:     percentile(sorted, 0.90),
		P95:     percentile(sorted, 0.95),
		P99:     percentile(sorted, 0.99),
		P999:    percentile(sorted, 0.999),
		Min:     sorted[0],
		Max:     sorted[len(sorted)-1],
		Mean:    mean(sorted),
		StdDev:  stdDev(sorted),
		Count:   len(sorted),
	}
}

func (lt *LatencyTracker) Clear() {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	lt.samples = lt.samples[:0]
	lt.lastClean = time.Now()
}

// LatencyPercentiles holds various percentile measurements
type LatencyPercentiles struct {
	P50    float64 `json:"p50"`
	P75    float64 `json:"p75"`
	P90    float64 `json:"p90"`
	P95    float64 `json:"p95"`
	P99    float64 `json:"p99"`
	P999   float64 `json:"p999"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Mean   float64 `json:"mean"`
	StdDev float64 `json:"stddev"`
	Count  int     `json:"count"`
}

// Helper functions for percentile calculations
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	
	index := p * float64(len(sorted)-1)
	lower := math.Floor(index)
	upper := math.Ceil(index)
	
	if lower == upper {
		return sorted[int(index)]
	}
	
	// Linear interpolation
	weight := index - lower
	return sorted[int(lower)]*(1-weight) + sorted[int(upper)]*weight
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func stdDev(values []float64) float64 {
	if len(values) <= 1 {
		return 0
	}
	
	m := mean(values)
	sumSquares := 0.0
	for _, v := range values {
		diff := v - m
		sumSquares += diff * diff
	}
	
	return math.Sqrt(sumSquares / float64(len(values)-1))
}
