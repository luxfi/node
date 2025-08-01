# Avalanche to Lux Consensus Structure Mapping

This document provides a comprehensive mapping between the original Avalanche consensus structure and the new Lux consensus structure, focusing on test portability and component alignment.

## Directory Structure Mapping

### Core Consensus Components

| Avalanche Path | Lux Path | Description |
|----------------|----------|-------------|
| `snow/consensus/snowman/` | `consensus/chain/` or `consensus/engine/chain/` | Linear chain consensus (Snowman) |
| `snow/consensus/avalanche/` | `consensus/engine/quasar/` | DAG-based consensus (Avalanche/Quasar) |
| `snow/consensus/snowball/` | `consensus/focus/` | Binary consensus primitive |
| `snow/consensus/snowstorm/` | `consensus/wave/` | Multi-value consensus |
| `snow/choices/` | `consensus/choices/` | Choice status definitions |
| `snow/validators/` | `consensus/validators/` | Validator management |

### Engine Components

| Avalanche Path | Lux Path | Description |
|----------------|----------|-------------|
| `snow/engine/snowman/` | `consensus/engine/chain/` | Snowman engine implementation |
| `snow/engine/avalanche/` | `consensus/engine/quasar/` | Avalanche/Quasar engine |
| `snow/engine/common/` | `consensus/engine/core/` | Common engine interfaces |
| `snow/engine/enginetest/` | `consensus/engine/enginetest/` | Engine test utilities |

### Test Utilities

| Avalanche Path | Lux Path | Description |
|----------------|----------|-------------|
| `snow/snowtest/` | `consensus/consensustest/` | General consensus test utilities |
| `snow/consensus/snowman/snowmantest/` | `consensus/chain/chaintest/` | Snowman/Chain test helpers |
| `snow/consensus/snowman/snowmanmock/` | `consensus/chain/chainmock/` | Snowman/Chain mocks |
| `snow/engine/enginetest/sender.go` | `consensus/engine/enginetest/sender.go` | Test sender implementation |

## Interface Mapping

### Consensus Interfaces

#### Snowman/Chain Consensus
```go
// Avalanche: snow/consensus/snowman/consensus.go
type Consensus interface {
    health.Checker
    Initialize(...)
    Add(Block) error
    RecordPoll(context.Context, bag.Bag[ids.ID]) error
    // ...
}

// Lux: consensus/chain/consensus.go
// Same interface, potentially with additional methods for Lux-specific features
```

#### Block Interface
```go
// Avalanche: snow/consensus/snowman/block.go
type Block interface {
    snow.Decidable
    Parent() ids.ID
    Verify(context.Context) error
    Bytes() []byte
    Height() uint64
    Timestamp() time.Time
}

// Lux: consensus/chain/block.go or consensus/engine/chain/block/
// Same core interface with possible extensions
```

### Engine Interfaces

#### Snowman Engine
```go
// Avalanche: snow/engine/snowman/engine.go
type Engine struct {
    Config
    // State management
    // Poll tracking
    // Block fetching
}

// Lux: consensus/engine/chain/engine.go
// Similar structure with Lux-specific optimizations
```

## Test Pattern Mapping

### Test Context Creation

#### Avalanche Pattern
```go
// snow/snowtest/context.go
func Context(tb testing.TB, chainID ids.ID) *snow.Context
func ConsensusContext(ctx *snow.Context) *snow.ConsensusContext
```

#### Lux Pattern
```go
// consensus/consensustest/context.go
// Similar functions adapted for Lux consensus parameters
```

### Test Block Creation

#### Avalanche Pattern
```go
// snow/consensus/snowman/snowmantest/block.go
func BuildChild(parent *Block) *Block
func BuildChain(length int) []*Block
func BuildDescendants(parent *Block, length int) []*Block
```

#### Lux Pattern
```go
// consensus/chain/chaintest/block_builder.go
// Similar builder patterns for test blocks
```

### Mock Generation

| Avalanche | Lux | Purpose |
|-----------|-----|---------|
| `snowmanmock/` | `chainmock/` | Generated mocks for interfaces |
| `enginetest/` | `engine/enginetest/` | Engine test utilities |
| Mock generation using `mockgen` | Same tooling | Interface mocking |

## Consensus Parameters Mapping

### Avalanche Parameters
```go
// snow/consensus/snowball/parameters.go
type Parameters struct {
    K                     int
    AlphaPreference      int
    AlphaConfidence      int
    Beta                  int
    MaxItemProcessingTime time.Duration
}
```

### Lux Parameters
```go
// consensus/params/networks.go
// Mainnet: 21 nodes, 9.63s consensus
// Testnet: 11 nodes, 6.3s consensus
// Local: 5 nodes, 3.69s consensus
```

## Key Test Files to Port

### Priority 1: Core Consensus Tests
1. `snow/consensus/snowman/consensus_test.go` → `consensus/chain/consensus_test.go`
2. `snow/consensus/snowman/topological_test.go` → `consensus/chain/topological_test.go`
3. `snow/consensus/snowman/network_test.go` → `consensus/chain/network_test.go`

### Priority 2: Engine Tests
1. `snow/engine/snowman/engine_test.go` → `consensus/engine/chain/engine_test.go`
2. `snow/engine/snowman/bootstrap/bootstrapper_test.go` → `consensus/engine/chain/bootstrap/bootstrapper_test.go`
3. `snow/engine/common/tracker/*_test.go` → `consensus/engine/core/tracker/*_test.go`

### Priority 3: Integration Tests
1. `snow/consensus/snowman/poll/*_test.go` → `consensus/chain/poll/*_test.go`
2. `snow/validators/*_test.go` → `consensus/validators/*_test.go`

## Migration Checklist

### For Each Test File:
- [ ] Update import paths from `avalanchego/snow/` to appropriate Lux paths
- [ ] Replace Avalanche-specific constants with Lux equivalents
- [ ] Update consensus parameters for Lux network configurations
- [ ] Adapt test utilities to use Lux test helpers
- [ ] Ensure mock generation works with new interfaces
- [ ] Update any hardcoded chain IDs or network IDs

### Test Utilities to Create:
- [ ] `consensustest/context.go` - Test context creation
- [ ] `chaintest/block_builder.go` - Test block builders
- [ ] `chaintest/consensus.go` - Test consensus helpers
- [ ] `enginetest/sender.go` - Test message sender
- [ ] Mock generation scripts for new interfaces

### Integration Points:
- [ ] Ensure test database uses BadgerDB (not LevelDB/PebbleDB)
- [ ] Update validator test states for Lux consensus
- [ ] Adapt network message handling for Lux protocol
- [ ] Include Lux-specific features in test scenarios

## Notes

1. **Naming Convention**: Avalanche's "snowman" maps to Lux's "chain" (linear consensus)
2. **Quasar Engine**: Lux's enhanced DAG consensus replaces Avalanche's avalanche consensus
3. **Test Database**: All tests should use BadgerDB as per Lux standards
4. **Network Parameters**: Tests must reflect Lux's network configurations (21/11/5 nodes)
5. **Additional Features**: Lux includes Ringtail (post-quantum) and other enhancements not in Avalanche

This mapping should be continuously updated as the porting process reveals additional correspondences or differences between the two architectures.