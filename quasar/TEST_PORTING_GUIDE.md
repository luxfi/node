# Avalanche to Lux Test Porting Guide

This guide provides detailed instructions for porting Avalanche consensus tests to the Lux codebase.

## Test Statistics
- **Avalanche**: ~85 test files in snow/
- **Lux Current**: ~36 test files in consensus/
- **Gap**: ~49 test files to evaluate and potentially port

## Test Structure Analysis

### Avalanche Test Patterns

#### 1. Consensus Test Structure
```go
// Example from consensus_test.go
func TestEngineQuery(t *testing.T) {
    require := require.New(t)
    
    // 1. Setup test environment
    engCfg := DefaultConfig(t)
    
    // 2. Initialize engine
    require.NoError(engCfg.engine.Start(context.Background(), 0))
    
    // 3. Create test blocks
    blk := &snowmantest.Block{
        Decidable: snowtest.Decidable{
            IDV:    ids.GenerateTestID(),
            Status: snowtest.Processing,
        },
        ParentV: snowmantest.GenesisID,
        HeightV: 1,
        BytesV:  []byte{1},
    }
    
    // 4. Execute test scenario
    require.NoError(engCfg.engine.issue(...)
    
    // 5. Verify results
    require.Equal(expected, actual)
}
```

#### 2. Engine Test Structure
```go
func TestEngine[Feature](t *testing.T) {
    // 1. Create engine configuration
    config := DefaultConfig(t)
    
    // 2. Setup mock expectations
    config.Sender.SendPushQueryF = func(...) { ... }
    
    // 3. Initialize components
    config.engine.Initialize(...)
    
    // 4. Trigger actions
    config.engine.PushQuery(...)
    
    // 5. Assert outcomes
    require.True(t, ...)
}
```

### Lux Test Patterns

#### 1. Use Lux-specific Parameters
```go
func TestConsensusMainnet(t *testing.T) {
    params := params.DefaultParameters // 21 nodes, 9.63s
    // ... test implementation
}

func TestConsensusTestnet(t *testing.T) {
    params := params.TestnetParameters // 11 nodes, 6.3s
    // ... test implementation
}
```

#### 2. BadgerDB Integration
```go
func setupTestDB(t *testing.T) database.Database {
    return badgerdb.New(t.TempDir())
}
```

## Test Categories to Port

### 1. Core Consensus Tests (Priority: HIGH)

| Test File | Purpose | Porting Notes |
|-----------|---------|---------------|
| `consensus_test.go` | Core consensus logic | Update parameters for Lux networks |
| `topological_test.go` | Block ordering | Ensure compatibility with Lux finality |
| `network_test.go` | Network consensus | Adapt for 21/11/5 node configurations |
| `mixed_test.go` | Mixed query handling | Update timing for Lux consensus speeds |

### 2. Engine Tests (Priority: HIGH)

| Test File | Purpose | Porting Notes |
|-----------|---------|---------------|
| `engine_test.go` | Engine operations | Update for chain/quasar engines |
| `bootstrap/bootstrapper_test.go` | Bootstrap process | Ensure BadgerDB compatibility |
| `getter/getter_test.go` | Block fetching | Adapt for Lux block structure |
| `syncer/utils_test.go` | Sync utilities | Update for Lux sync protocols |

### 3. Poll Tests (Priority: MEDIUM)

| Test File | Purpose | Porting Notes |
|-----------|---------|---------------|
| `poll/set_test.go` | Poll management | Update poll durations |
| `poll/early_term_test.go` | Early termination | Adapt for Lux finality rules |
| `poll/no_early_term_test.go` | Full poll cycles | Consider Lux timing |

### 4. Validator Tests (Priority: MEDIUM)

| Test File | Purpose | Porting Notes |
|-----------|---------|---------------|
| `validators/manager_test.go` | Validator management | Update for Lux validator sets |
| `validators/set_test.go` | Validator set operations | Consider 21/11/5 configurations |

### 5. Integration Tests (Priority: LOW)

| Test File | Purpose | Porting Notes |
|-----------|---------|---------------|
| `bootstrap/interval/*_test.go` | Interval bootstrapping | Update for Lux intervals |
| `ancestor/*_test.go` | Ancestor management | Ensure compatibility |

## Test Helper Creation Guide

### 1. Create `consensustest/builders.go`
```go
package consensustest

import (
    "testing"
    "github.com/lux/node/consensus/chain"
    "github.com/lux/node/consensus/params"
)

// BuildMainnetConsensus creates a consensus instance with mainnet params
func BuildMainnetConsensus(t *testing.T) chain.Consensus {
    return buildConsensus(t, params.DefaultParameters)
}

// BuildTestnetConsensus creates a consensus instance with testnet params
func BuildTestnetConsensus(t *testing.T) chain.Consensus {
    return buildConsensus(t, params.TestnetParameters)
}

// BuildLocalConsensus creates a consensus instance with local params
func BuildLocalConsensus(t *testing.T) chain.Consensus {
    return buildConsensus(t, params.LocalParameters)
}
```

### 2. Create `chaintest/block_factory.go`
```go
package chaintest

// BlockFactory creates test blocks
type BlockFactory struct {
    currentHeight uint64
    parentID      ids.ID
}

// NewBlockFactory creates a new block factory
func NewBlockFactory() *BlockFactory {
    return &BlockFactory{
        currentHeight: 0,
        parentID:      GenesisID,
    }
}

// NextBlock creates the next block in sequence
func (f *BlockFactory) NextBlock() *Block {
    f.currentHeight++
    blk := &Block{
        HeightV:    f.currentHeight,
        ParentV:    f.parentID,
        TimestampV: time.Now(),
        IDV:        ids.GenerateTestID(),
    }
    f.parentID = blk.ID()
    return blk
}
```

### 3. Create `enginetest/mock_sender.go`
```go
package enginetest

// MockSender for engine tests
type MockSender struct {
    t *testing.T
    
    // Recorded calls
    PushQueryCalls []PushQueryCall
    PullQueryCalls []PullQueryCall
    ChitsCalls     []ChitsCall
}

// Assertion helpers
func (m *MockSender) AssertPushQueryCount(expected int) {
    require.Len(m.t, m.PushQueryCalls, expected)
}
```

## Migration Workflow

### Phase 1: Setup Test Infrastructure
1. Create test helper packages:
   - [x] `consensustest/` - General test utilities
   - [ ] `chaintest/` - Chain-specific test helpers
   - [ ] `chainmock/` - Generated mocks
   - [ ] `enginetest/` - Engine test utilities

2. Port core test utilities:
   - [ ] Context builders
   - [ ] Block factories
   - [ ] Mock senders
   - [ ] Validator helpers

### Phase 2: Port Core Tests
1. Start with high-priority consensus tests
2. Update imports and parameters
3. Adapt test scenarios for Lux
4. Ensure all tests pass

### Phase 3: Port Engine Tests
1. Port engine initialization tests
2. Update message handling tests
3. Adapt bootstrap tests
4. Port sync tests

### Phase 4: Integration Tests
1. Port complex scenarios
2. Add Lux-specific tests
3. Performance benchmarks
4. Stress tests

## Common Porting Tasks

### 1. Update Imports
```go
// Before
import "github.com/ava-labs/avalanchego/snow/consensus/snowman"

// After
import "github.com/lux/node/consensus/chain"
```

### 2. Update Parameters
```go
// Before
params := snowball.Parameters{K: 20, Alpha: 15}

// After
params := params.DefaultParameters // or TestnetParameters
```

### 3. Update Database
```go
// Before
db := memdb.New()

// After
db := badgerdb.New(t.TempDir())
```

### 4. Update Timing
```go
// Before
timeout := 2 * time.Second

// After (for mainnet)
timeout := 9630 * time.Millisecond
```

## Test Validation Checklist

For each ported test:
- [ ] Imports updated to Lux packages
- [ ] Parameters match Lux network configs
- [ ] Database uses BadgerDB
- [ ] Timing aligns with consensus speeds
- [ ] Mock interfaces match Lux interfaces
- [ ] Test passes in isolation
- [ ] Test passes in full suite
- [ ] No race conditions
- [ ] Proper cleanup

## New Tests for Lux Features

### 1. Quasar Engine Tests
- DAG construction
- Quantum-resistant operations
- Parallel processing

### 2. Ringtail Integration
- Post-quantum signatures
- Key migration
- Performance impact

### 3. Network Configuration Tests
- 21-node mainnet scenarios
- 11-node testnet scenarios
- 5-node local scenarios

### 4. Performance Tests
- 9.63s mainnet consensus
- 6.3s testnet consensus
- 3.69s local consensus

## Notes

1. **Test Naming**: Keep similar names for easy correlation
2. **Coverage**: Aim for >80% test coverage
3. **Benchmarks**: Add benchmarks for critical paths
4. **Documentation**: Document any significant changes
5. **Backwards Compatibility**: Note any breaking changes

This guide should be updated as the porting process progresses and new patterns emerge.