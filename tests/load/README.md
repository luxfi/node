# Load Testing Framework

This directory contains a comprehensive load testing framework for the Lux network, designed to measure and optimize transaction throughput, latency, and overall network performance.

## Overview

The load testing framework provides sophisticated tools to generate, execute, and analyze various load patterns against Lux nodes. It supports TPS (Transactions Per Second) targeting, latency percentile tracking, and multiple load generation scenarios.

## Key Features

- **TPS Targeting**: Precise control over transaction throughput with feedback-based adjustments
- **Latency Tracking**: Real-time percentile calculations (P50, P75, P90, P95, P99, P999)
- **Multiple Load Patterns**: Constant rate, ramp-up, burst, wave, and step patterns
- **Comprehensive Metrics**: Success rates, error distribution, and performance statistics
- **Scenario Testing**: Pre-configured test scenarios for common use cases

## Components

### Core Components

- **Agent** (`agent.go`): Coordinates transaction issuance and confirmation tracking
- **Orchestrator** (`orchestrator.go`): Manages load generation across multiple agents with TPS control
- **Tracker** (`tracker.go`): Monitors transaction status, latency, and success rates
- **Metrics** (`metrics.go`): Advanced metrics collection with percentile tracking
- **Generator** (`generator.go`): Load pattern generation with various strategies
- **Wallet** (`wallet.go`): Transaction signing and submission management

### Test Scenarios

- **Sustained Load**: Constant TPS over a specified duration
- **Ramp-Up**: Gradually increasing TPS to find maximum capacity
- **Spike Testing**: Sudden burst of transactions to test recovery
- **Stress Testing**: Finding system breaking points
- **Soak Testing**: Long-duration stability testing

## Usage

### Running All Tests

```bash
# Run all load test scenarios
go test ./tests/load -v

# Run specific test scenario
go test ./tests/load -v -run TestScenarioSustainedLoad

# Run with custom timeout
go test ./tests/load -v -timeout 30m
```

### Configuration

#### Basic Configuration

```go
config := OrchestratorConfig{
    MaxTPS:           5000,     // Maximum target TPS
    MinTPS:           1000,     // Starting TPS
    Step:             1000,     // TPS increment step
    TxRateMultiplier: 1.3,      // Overprovision factor
    SustainedTime:    20 * time.Second,
    MaxAttempts:      3,        // Retry attempts
    Terminate:        true,     // Stop at max TPS
}
```

#### Load Patterns

```go
// Constant rate pattern
pattern := NewConstantRatePattern(1000) // 1000 TPS

// Ramp pattern
pattern := NewRampPattern(100, 5000, 5*time.Minute)

// Burst pattern
pattern := NewBurstPattern(1000, 10*time.Second)

// Wave pattern (sinusoidal)
pattern := NewWavePattern(500, 2000, 30*time.Second)

// Step pattern
pattern := NewStepPattern([]float64{100, 500, 1000, 2000}, 30*time.Second)
```

### Example Test Scenarios

#### 1. Sustained Load Test

```go
func TestSustainedLoad(t *testing.T) {
    config := OrchestratorConfig{
        MaxTPS: 1000,
        MinTPS: 1000,
        Step: 0,
        TxRateMultiplier: 1.0,
        SustainedTime: 5 * time.Second,
        Terminate: false,
    }
    
    // Run for 60 seconds at 1000 TPS
    ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
    defer cancel()
    
    orchestrator.Execute(ctx)
}
```

#### 2. Finding Maximum TPS

```go
func TestFindMaxTPS(t *testing.T) {
    config := OrchestratorConfig{
        MaxTPS: 10000,
        MinTPS: 1000,
        Step: 500,
        TxRateMultiplier: 1.2,
        SustainedTime: 10 * time.Second,
        MaxAttempts: 3,
        Terminate: true,
    }
    
    orchestrator.Execute(ctx)
    maxTPS := orchestrator.GetMaxObservedTPS()
}
```

#### 3. Spike Recovery Test

```go
func TestSpikeRecovery(t *testing.T) {
    // Burst mode with MaxTPS = -1
    config := OrchestratorConfig{
        MaxTPS: -1,
        MinTPS: 10000,  // Burst size
        Step: 0,
        TxRateMultiplier: 1.0,
        SustainedTime: 5 * time.Second,
        Terminate: true,
    }
    
    orchestrator.Execute(ctx)
}
```

## Metrics and Reporting

### Available Metrics

- **Transaction Metrics**:
  - Total issued, confirmed, failed
  - Success rate percentage
  - Error distribution by type

- **Performance Metrics**:
  - Current TPS
  - Average TPS
  - Peak TPS
  - TPS over time

- **Latency Metrics**:
  - Percentiles: P50, P75, P90, P95, P99, P999
  - Min, Max, Mean, StdDev
  - Histogram buckets

### Accessing Metrics

```go
// Get latency percentiles
percentiles := metrics.GetLatencyPercentiles()
fmt.Printf("P50: %.2fms, P95: %.2fms, P99: %.2fms\n", 
    percentiles.P50, percentiles.P95, percentiles.P99)

// Get transaction counts
confirmed := tracker.GetObservedConfirmed()
failed := tracker.GetObservedFailed()
issued := tracker.GetObservedIssued()

// Calculate success rate
successRate := float64(confirmed) / float64(confirmed + failed)
```

### Load Report Generation

```go
report := LoadReport{
    StartTime:          startTime,
    EndTime:            time.Now(),
    Duration:           duration,
    TotalTransactions:  issued,
    SuccessfulTx:       confirmed,
    FailedTx:           failed,
    AverageTPS:         avgTPS,
    PeakTPS:            peakTPS,
    SuccessRate:        successRate,
    LatencyPercentiles: percentiles,
}

fmt.Println(report.String())
```

## Performance Targets

### Recommended Baselines

- **Minimum Sustained TPS**: 1000
- **Target TPS**: 5000+
- **P95 Latency**: < 500ms
- **P99 Latency**: < 1000ms
- **Success Rate**: > 99.5%

### Stress Testing Targets

- **Burst Capacity**: 10x normal load for 10 seconds
- **Recovery Time**: < 30 seconds after spike
- **Degradation Threshold**: 80% of peak TPS
- **Stability Duration**: 1 hour sustained load

## Advanced Usage

### Custom Load Patterns

```go
type CustomPattern struct {
    // Your pattern implementation
}

func (p *CustomPattern) NextDelay() time.Duration {
    // Calculate delay for next transaction
}

func (p *CustomPattern) ShouldSend() bool {
    // Determine if transaction should be sent
}

func (p *CustomPattern) Reset() {
    // Reset pattern state
}
```

### TPS Controller with Feedback

```go
controller := NewTPSController(1000)

// Update with actual counts
controller.UpdateActualTPS(confirmedCount)

// Get adjusted delay
delay := controller.GetDelay()

// Get statistics
target, actual, adjustment := controller.GetStats()
```

## Troubleshooting

### Common Issues

1. **Low TPS Achievement**
   - Increase `TxRateMultiplier` (e.g., 1.5)
   - Add more agents/workers
   - Check network latency

2. **High Failure Rate**
   - Reduce target TPS
   - Increase `SustainedTime` for stability
   - Check node resources

3. **Inconsistent Results**
   - Use longer `SustainedTime` (20+ seconds)
   - Increase sample size
   - Check for network congestion

## Contributing

When adding new test scenarios:

1. Implement in `scenarios_test.go`
2. Add corresponding mock helpers if needed
3. Update metrics collection as appropriate
4. Document expected behavior and targets
5. Ensure tests are idempotent and cleanup properly