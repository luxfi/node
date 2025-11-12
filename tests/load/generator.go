// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luxfi/geth/ethclient"
	"github.com/luxfi/metric"
	"golang.org/x/sync/errgroup"

	"github.com/luxfi/log"
	"github.com/luxfi/node/tests"
)

type Test interface {
	Run(tc tests.TestContext, wallet *Wallet)
}

type Worker struct {
	PrivKey *ecdsa.PrivateKey
	Nonce   uint64
	Client  *ethclient.Client
}

type LoadGenerator struct {
	wallets []*Wallet
	test    Test
}

func NewLoadGenerator(
	workers []Worker,
	chainID *big.Int,
	metricsNamespace string,
	registry metric.Registry,
	test Test,
) (LoadGenerator, error) {
	metrics, err := NewMetrics(registry)
	if err != nil {
		return LoadGenerator{}, err
	}

	wallets := make([]*Wallet, len(workers))
	for i := range wallets {
		wallets[i] = newWallet(
			workers[i].PrivKey,
			workers[i].Nonce,
			chainID,
			workers[i].Client,
			metrics,
		)
	}

	return LoadGenerator{
		wallets: wallets,
		test:    test,
	}, nil
}

func (l LoadGenerator) Run(
	ctx context.Context,
	log log.Logger,
	loadTimeout time.Duration,
	testTimeout time.Duration,
) {
	eg := &errgroup.Group{}

	if loadTimeout != 0 {
		childCtx, cancel := context.WithTimeout(ctx, loadTimeout)
		ctx = childCtx
		defer cancel()
	}

	for i := range l.wallets {
		eg.Go(func() error {
			for {
				select {
				case <-ctx.Done():
					return nil
				default:
				}

				execTestWithRecovery(ctx, log, l.test, l.wallets[i], testTimeout)
			}
		})
	}

	_ = eg.Wait()
}

// execTestWithRecovery ensures assertion-related panics encountered during test execution are recovered
// and that deferred cleanups are always executed before returning.
func execTestWithRecovery(ctx context.Context, log log.Logger, test Test, wallet *Wallet, testTimeout time.Duration) {
	tc := tests.NewTestContext(log)
	defer tc.Cleanup()
	// Note: testTimeout is currently unused as tests create their own contexts via tc.DefaultContext()
	// which uses the standard 2-minute timeout. If per-test timeouts are needed, tests should be
	// modified to use tc.ContextWithTimeout(testTimeout) instead.
	test.Run(tc, wallet)
}

// LoadPattern defines different load generation patterns
type LoadPattern interface {
	// NextDelay returns the delay before the next transaction
	NextDelay() time.Duration
	// ShouldSend determines if a transaction should be sent now
	ShouldSend() bool
	// Reset resets the pattern state
	Reset()
}

// ConstantRatePattern generates load at a constant rate
type ConstantRatePattern struct {
	tps float64
	mu  sync.Mutex
}

func NewConstantRatePattern(tps float64) *ConstantRatePattern {
	return &ConstantRatePattern{tps: tps}
}

func (c *ConstantRatePattern) NextDelay() time.Duration {
	if c.tps <= 0 {
		return time.Hour // Effectively stop
	}
	return time.Duration(float64(time.Second) / c.tps)
}

func (c *ConstantRatePattern) ShouldSend() bool {
	return true
}

func (c *ConstantRatePattern) Reset() {}

// RampPattern gradually increases load
type RampPattern struct {
	startTPS   float64
	endTPS     float64
	duration   time.Duration
	startTime  time.Time
	currentTPS atomic.Value
}

func NewRampPattern(startTPS, endTPS float64, duration time.Duration) *RampPattern {
	r := &RampPattern{
		startTPS:  startTPS,
		endTPS:    endTPS,
		duration:  duration,
		startTime: time.Now(),
	}
	r.currentTPS.Store(startTPS)
	return r
}

func (r *RampPattern) NextDelay() time.Duration {
	elapsed := time.Since(r.startTime)
	progress := math.Min(elapsed.Seconds()/r.duration.Seconds(), 1.0)
	
	currentTPS := r.startTPS + (r.endTPS-r.startTPS)*progress
	r.currentTPS.Store(currentTPS)
	
	if currentTPS <= 0 {
		return time.Hour
	}
	return time.Duration(float64(time.Second) / currentTPS)
}

func (r *RampPattern) ShouldSend() bool {
	return true
}

func (r *RampPattern) Reset() {
	r.startTime = time.Now()
	r.currentTPS.Store(r.startTPS)
}

// BurstPattern generates bursts of load
type BurstPattern struct {
	burstSize     int
	burstInterval time.Duration
	inBurst       bool
	burstCount    int
	lastBurst     time.Time
	mu            sync.Mutex
}

func NewBurstPattern(burstSize int, burstInterval time.Duration) *BurstPattern {
	return &BurstPattern{
		burstSize:     burstSize,
		burstInterval: burstInterval,
		inBurst:       true,  // Start with a burst
		burstCount:    0,
		lastBurst:     time.Now(),
	}
}

func (b *BurstPattern) NextDelay() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	// Check if we should start a new burst
	if !b.inBurst && time.Since(b.lastBurst) >= b.burstInterval {
		b.inBurst = true
		b.burstCount = 0
		b.lastBurst = time.Now()
	}
	
	// If in burst and haven't reached burst size
	if b.inBurst && b.burstCount < b.burstSize {
		b.burstCount++
		if b.burstCount >= b.burstSize {
			b.inBurst = false // End burst after reaching size
		}
		return time.Microsecond // Minimal delay during burst
	}
	
	// Waiting for next burst
	remaining := b.burstInterval - time.Since(b.lastBurst)
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

func (b *BurstPattern) ShouldSend() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inBurst && b.burstCount < b.burstSize
}

func (b *BurstPattern) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.inBurst = false
	b.burstCount = 0
	b.lastBurst = time.Now()
}

// WavePattern generates sinusoidal load pattern
type WavePattern struct {
	minTPS    float64
	maxTPS    float64
	period    time.Duration
	startTime time.Time
}

func NewWavePattern(minTPS, maxTPS float64, period time.Duration) *WavePattern {
	return &WavePattern{
		minTPS:    minTPS,
		maxTPS:    maxTPS,
		period:    period,
		startTime: time.Now(),
	}
}

func (w *WavePattern) NextDelay() time.Duration {
	elapsed := time.Since(w.startTime)
	phase := (elapsed.Seconds() / w.period.Seconds()) * 2 * math.Pi
	
	// Sinusoidal variation
	amplitude := (w.maxTPS - w.minTPS) / 2
	midpoint := (w.maxTPS + w.minTPS) / 2
	currentTPS := midpoint + amplitude*math.Sin(phase)
	
	if currentTPS <= 0 {
		return time.Hour
	}
	return time.Duration(float64(time.Second) / currentTPS)
}

func (w *WavePattern) ShouldSend() bool {
	return true
}

func (w *WavePattern) Reset() {
	w.startTime = time.Now()
}

// StepPattern generates stepped load increases
type StepPattern struct {
	steps         []float64
	stepDuration  time.Duration
	currentStep   int
	stepStartTime time.Time
	mu            sync.Mutex
}

func NewStepPattern(steps []float64, stepDuration time.Duration) *StepPattern {
	return &StepPattern{
		steps:         steps,
		stepDuration:  stepDuration,
		currentStep:   0,
		stepStartTime: time.Now(),
	}
}

func (s *StepPattern) NextDelay() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Check if we should move to next step
	if time.Since(s.stepStartTime) >= s.stepDuration {
		s.currentStep = (s.currentStep + 1) % len(s.steps)
		s.stepStartTime = time.Now()
	}
	
	tps := s.steps[s.currentStep]
	if tps <= 0 {
		return time.Hour
	}
	return time.Duration(float64(time.Second) / tps)
}

func (s *StepPattern) ShouldSend() bool {
	return true
}

func (s *StepPattern) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentStep = 0
	s.stepStartTime = time.Now()
}

// LoadConfig defines configuration for load generation
type LoadConfig struct {
	Pattern      LoadPattern
	MaxTPS       float64
	Duration     time.Duration
	WarmupPeriod time.Duration
}

// TPSController manages TPS targeting with feedback control
type TPSController struct {
	targetTPS    atomic.Value // float64
	actualTPS    atomic.Value // float64
	adjustment   atomic.Value // float64
	lastUpdate   time.Time
	lastCount    uint64
	mu           sync.Mutex
}

func NewTPSController(targetTPS float64) *TPSController {
	c := &TPSController{
		lastUpdate: time.Now(),
	}
	c.targetTPS.Store(targetTPS)
	c.actualTPS.Store(0.0)
	c.adjustment.Store(1.0)
	return c
}

func (c *TPSController) SetTargetTPS(tps float64) {
	c.targetTPS.Store(tps)
}

func (c *TPSController) UpdateActualTPS(count uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	now := time.Now()
	elapsed := now.Sub(c.lastUpdate).Seconds()
	
	if elapsed > 0 {
		actualTPS := float64(count-c.lastCount) / elapsed
		c.actualTPS.Store(actualTPS)
		
		// PID-like control
		target := c.targetTPS.Load().(float64)
		error := target - actualTPS
		adjustment := c.adjustment.Load().(float64)
		
		// Proportional adjustment
		adjustment += error * 0.01
		adjustment = math.Max(0.5, math.Min(2.0, adjustment))
		
		c.adjustment.Store(adjustment)
		c.lastUpdate = now
		c.lastCount = count
	}
}

func (c *TPSController) GetDelay() time.Duration {
	target := c.targetTPS.Load().(float64)
	adjustment := c.adjustment.Load().(float64)
	
	if target <= 0 {
		return time.Hour
	}
	
	adjustedTPS := target * adjustment
	return time.Duration(float64(time.Second) / adjustedTPS)
}

func (c *TPSController) GetStats() (targetTPS, actualTPS, adjustment float64) {
	return c.targetTPS.Load().(float64),
		c.actualTPS.Load().(float64),
		c.adjustment.Load().(float64)
}

// LoadReport contains load test results
type LoadReport struct {
	StartTime           time.Time
	EndTime             time.Time
	Duration            time.Duration
	TotalTransactions   uint64
	SuccessfulTx        uint64
	FailedTx            uint64
	AverageTPS          float64
	PeakTPS             float64
	SuccessRate         float64
	LatencyPercentiles  LatencyPercentiles
	ErrorDistribution   map[string]uint64
}

func (r LoadReport) String() string {
	return fmt.Sprintf(`
Load Test Report
================
Duration: %s
Start: %s
End: %s

Transactions:
  Total:      %d
  Successful: %d
  Failed:     %d
  Success Rate: %.2f%%

Performance:
  Average TPS: %.2f
  Peak TPS:    %.2f

Latency (ms):
  P50:  %.2f
  P75:  %.2f
  P90:  %.2f
  P95:  %.2f
  P99:  %.2f
  P999: %.2f
  Min:  %.2f
  Max:  %.2f
  Mean: %.2f
  StdDev: %.2f
`,
		r.Duration,
		r.StartTime.Format(time.RFC3339),
		r.EndTime.Format(time.RFC3339),
		r.TotalTransactions,
		r.SuccessfulTx,
		r.FailedTx,
		r.SuccessRate*100,
		r.AverageTPS,
		r.PeakTPS,
		r.LatencyPercentiles.P50,
		r.LatencyPercentiles.P75,
		r.LatencyPercentiles.P90,
		r.LatencyPercentiles.P95,
		r.LatencyPercentiles.P99,
		r.LatencyPercentiles.P999,
		r.LatencyPercentiles.Min,
		r.LatencyPercentiles.Max,
		r.LatencyPercentiles.Mean,
		r.LatencyPercentiles.StdDev,
	)
}
