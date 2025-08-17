# LLM.md - Lux Node Project

## Project Overview
This is the Lux blockchain node implementation, a fork of Avalanche with modifications for the Lux network ecosystem. The project is written in Go and implements a multi-chain blockchain platform with support for multiple subnets.

## Architecture

### Core Components
- **Node Implementation** (`/home/z/work/lux/node/`)
  - Main blockchain node with consensus, networking, and VM support
  - Modified from Avalanche to support Lux-specific features
  - Version: v0.1.0-lux.15

### Key Modules
- **consensus/** - Consensus protocols and validator management
- **vms/** - Virtual Machines including platformvm, avm, and evm
- **chains/** - Chain management and atomic operations
- **api/** - RPC and REST API implementations
- **network/** - P2P networking and gossip protocols
- **database/** - Database backends (LevelDB, Memory, Prefix)
- **utils/** - Utility functions and helpers

## Current State (as of last work session)

### Test Coverage Status
- **Overall Pass Rate**: ~80% (estimated)
- Major areas fixed:
  - Import cycles resolved
  - Mock implementations generated
  - Context usage patterns partially standardized with testcontext package
  - Interface adapters created
  - State package mocks regenerated
  - Clock type issues resolved
  - TXS executor tests building (but runtime issues remain)

### Critical Known Issues
1. **Context Type Mismatch**: Major breaking change - tests expect a struct-based context with fields (Lock, SharedMemory, ChainID, etc.) but the codebase has moved to standard context.Context pattern
2. **VM Initialize Signature**: VM.Initialize expects linearblock.ChainContext but tests pass a different type
3. **Consensus Package Changes**: The consensus package has been refactored to use context values instead of struct fields
4. **Test Infrastructure**: Many test files (helpers_test.go, acceptor_test.go) are temporarily disabled due to context issues

### Skipped Test Files
- `/vms/platformvm/block/executor/helpers_test.go.skip`
- `/vms/platformvm/block/executor/acceptor_test.go.skip`
- `/vms/platformvm/txs/executor/state_changes_test.go.skip` (uses removed ValidatorFeeConfig/FeeState)

### Known Issues
1. **Mock Interfaces**: Some mocks don't match current State/Chain interfaces
2. **Removed Types**: Tests reference removed types like `SubnetToL1Conversion`
3. **Context Patterns**: Major refactoring needed - tests expect old-style context struct
4. **SharedMemory Interface**: Requires adapters for interface compatibility
5. **Clock Types**: Mismatch between consensus and node clock types

## Development Patterns

### Testing
- Use `go test ./... -count=1` to run all tests
- Mock generation: `go generate ./...` in package directories
- Test packages use `_test` suffix to avoid import cycles

### Interface Patterns
```go
// Adapter pattern for interface compatibility
type stateReaderAdapter struct {
    state.State
}

func (s *stateReaderAdapter) GetL1Validator(id ids.ID) (L1ValidatorInfo, error) {
    validator, err := s.State.GetL1Validator(id)
    if err != nil {
        return nil, err
    }
    return &validator, nil
}
```

### Context Usage
```go
// Use consensus helpers instead of struct literals
ctx := context.Background()
ctx = consensus.WithIDs(ctx, consensus.IDs{
    NetworkID:  1,
    ChainID:    constants.PlatformChainID,
    LUXAssetID: luxAssetID,
})
```

## Common Tasks

### Running Tests
```bash
# Run all tests
go test ./... -count=1

# Run specific package tests
go test ./vms/platformvm/state -count=1

# Check test coverage
go test ./... -cover
```

### Generating Mocks
```bash
cd vms/platformvm/state
go generate ./...
```

### Building
```bash
go build -o luxd ./app
```

## Important Files
- **consensus/ctx.go** - Context management and consensus IDs
- **vms/platformvm/state/state.go** - State interface definitions
- **vms/platformvm/state/mocks_generate_test.go** - Mock generation directives
- **vms/platformvm/network/warp.go** - Warp message handling

## Dependencies
- Go 1.21.12 or higher
- uber/mock for mock generation
- protobuf for message serialization
- Various Lux-specific packages with -lux.15 tags

## Notes for Future Development

### When Adding New Tests
1. Check if mocks need regeneration
2. Use proper context patterns (no struct literals)
3. Create adapters for interface mismatches
4. Use `_test` package suffix if import cycles occur

### Common Fixes
- **Import Cycle**: Move test to separate package with `_test` suffix
- **Missing Mock Methods**: Regenerate mocks or create adapters
- **Context Issues**: Use consensus.WithIDs() instead of literals
- **Interface Mismatch**: Create adapter structs

### Areas Needing Attention
1. Complete mock regeneration for all packages
2. Update tests for removed functionality
3. Standardize SharedMemory interface usage
4. Fix remaining 29 failing test packages

## Related Projects
- **/home/z/work/lux/geth** - C-Chain implementation
- **/home/z/work/lux/evm** - Subnet EVM
- **/home/z/work/lux/cli** - Management CLI
- **/home/z/work/lux/ava/avalanchego** - Upstream Avalanche reference