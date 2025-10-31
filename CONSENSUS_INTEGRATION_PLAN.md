# Consensus Integration Implementation Plan

## Overview

This document provides a detailed implementation plan for integrating the standalone consensus package with the Lux node. The plan is structured in phases to minimize risk and ensure systematic progress.

## Current Build Errors Analysis

### Critical Integration Errors

1. **Context-Related Errors**
   - `vms/components/lux/addresses.go:53`: Unknown field bcLookup in addressManager
   - `vms/components/lux/addresses.go:62,86,104`: undefined: consensus (helper functions)
   - Missing context helper functions for GetChainID, GetNetworkID

2. **VM Interface Errors**
   - `api/server/server.go`: VM missing NewHTTPHandler method
   - VM interface mismatch between consensus and node expectations

3. **Validator State Errors**
   - `vms/platformvm/warp/validator.go:71`: undefined: subnetID
   - `vms/platformvm/warp/validator.go:77`: Type mismatch for ValidatorData
   - `vms/platformvm/warp/validator.go:209`: ValidatorState missing GetNetID method

4. **Import/Type Errors**
   - Multiple duplicate imports and redeclarations
   - Math package function mismatches (Add, Mul undefined)
   - Metric interface incompatibilities

## Phase 1: Foundation (Days 1-2)

### Objective
Establish the adapter layer foundation and fix basic compilation errors.

### Tasks

#### 1.1 Create Adapter Package Structure
```bash
# Create directory structure
mkdir -p /Users/z/work/lux/node/consensus/adapter
mkdir -p /Users/z/work/lux/node/consensus/bridge
mkdir -p /Users/z/work/lux/node/consensus/utils
```

#### 1.2 Implement Core Context Adapter

**File**: `/node/consensus/adapter/context.go`

```go
package adapter

import (
    "sync"
    
    consensusctx "github.com/luxfi/consensus/context"
    "github.com/luxfi/ids"
    "github.com/luxfi/node/vms/components/lux"
)

// ContextWithBCLookup extends consensus context with BCLookup
type ContextWithBCLookup struct {
    *consensusctx.Context
    bcLookup lux.BCLookup
    lock     sync.RWMutex
}

// NewContextWithBCLookup creates an extended context
func NewContextWithBCLookup(ctx *consensusctx.Context) *ContextWithBCLookup {
    return &ContextWithBCLookup{
        Context:  ctx,
        bcLookup: NewDefaultBCLookup(),
    }
}

// SetBCLookup sets the blockchain lookup implementation
func (c *ContextWithBCLookup) SetBCLookup(lookup lux.BCLookup) {
    c.lock.Lock()
    defer c.lock.Unlock()
    c.bcLookup = lookup
}

// GetBCLookup returns the blockchain lookup
func (c *ContextWithBCLookup) GetBCLookup() lux.BCLookup {
    c.lock.RLock()
    defer c.lock.RUnlock()
    return c.bcLookup
}
```

#### 1.3 Implement Helper Functions

**File**: `/node/consensus/adapter/helpers.go`

```go
package adapter

import (
    consensusctx "github.com/luxfi/consensus/context"
    "github.com/luxfi/ids"
)

// Package-level helper functions for backward compatibility
var consensus = struct{}{}

// GetChainID extracts chain ID from consensus context
func GetChainID(ctx *consensusctx.Context) ids.ID {
    if ctx == nil {
        return ids.Empty
    }
    return ctx.ChainID
}

// GetNetworkID extracts network ID from consensus context
func GetNetworkID(ctx *consensusctx.Context) uint32 {
    if ctx == nil {
        return 0
    }
    return ctx.QuantumID
}

// GetNetID extracts net ID from consensus context
func GetNetID(ctx *consensusctx.Context) ids.ID {
    if ctx == nil {
        return ids.Empty
    }
    return ctx.NetID
}
```

### Deliverables
- [ ] Adapter package structure created
- [ ] Core context adapter implemented
- [ ] Helper functions available
- [ ] Basic compilation errors in addresses.go resolved

## Phase 2: Interface Adapters (Days 3-4)

### Objective
Implement VM and ValidatorState adapters to bridge interface gaps.

### Tasks

#### 2.1 VM Adapter Implementation

**File**: `/node/consensus/adapter/vm_adapter.go`

```go
package adapter

import (
    "context"
    "fmt"
    "net/http"
    
    "github.com/luxfi/consensus/core"
    consensusctx "github.com/luxfi/consensus/context"
    "github.com/luxfi/database/manager"
)

// VMAdapter bridges consensus VM to node VM interface
type VMAdapter struct {
    core.VM
    handlers       map[string]http.Handler
    staticHandlers map[string]http.Handler
}

// NewVMAdapter wraps a consensus VM with node-required methods
func NewVMAdapter(vm core.VM) *VMAdapter {
    return &VMAdapter{
        VM:             vm,
        handlers:       make(map[string]http.Handler),
        staticHandlers: make(map[string]http.Handler),
    }
}

// NewHTTPHandler creates HTTP handler for VM
func (v *VMAdapter) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
    handlers, err := v.CreateHandlers(ctx)
    if err != nil {
        return nil, fmt.Errorf("failed to create handlers: %w", err)
    }
    
    mux := http.NewServeMux()
    for path, handler := range handlers {
        mux.Handle(path, handler)
    }
    
    return mux, nil
}
```

#### 2.2 ValidatorState Adapter

**File**: `/node/consensus/adapter/validator_adapter.go`

```go
package adapter

import (
    "context"
    "fmt"
    
    "github.com/luxfi/consensus/validators"
    "github.com/luxfi/ids"
    "github.com/luxfi/node/vms/platformvm/warp"
)

// ValidatorStateAdapter adapts consensus ValidatorState for node use
type ValidatorStateAdapter struct {
    state validators.State
}

// NewValidatorStateAdapter creates a validator state adapter
func NewValidatorStateAdapter(state validators.State) *ValidatorStateAdapter {
    return &ValidatorStateAdapter{
        state: state,
    }
}

// GetNetID returns the network ID for a chain
func (v *ValidatorStateAdapter) GetNetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
    // Implementation to derive net ID from chain ID
    // This might require looking up chain configuration
    return v.state.GetSubnetID(ctx, chainID)
}

// GetValidatorSet returns validators in warp.ValidatorData format
func (v *ValidatorStateAdapter) GetValidatorSet(
    ctx context.Context,
    height uint64,
    subnetID ids.ID,
) (map[ids.NodeID]*warp.ValidatorData, error) {
    vdrs, _, err := v.state.GetValidatorSet(ctx, height, subnetID)
    if err != nil {
        return nil, fmt.Errorf("failed to get validator set: %w", err)
    }
    
    result := make(map[ids.NodeID]*warp.ValidatorData)
    for nodeID, vdr := range vdrs {
        result[nodeID] = &warp.ValidatorData{
            NodeID:    nodeID,
            PublicKey: vdr.PublicKey,
            Weight:    vdr.Weight,
        }
    }
    
    return result, nil
}
```

### Deliverables
- [ ] VM adapter with NewHTTPHandler implemented
- [ ] ValidatorState adapter with GetNetID implemented
- [ ] Type conversion utilities tested
- [ ] Server registration errors resolved

## Phase 3: Fix Component Integration (Days 5-6)

### Objective
Update node components to use adapters and fix remaining integration issues.

### Tasks

#### 3.1 Update Address Manager

**File**: `/node/vms/components/lux/addresses.go` (modifications)

```go
package lux

import (
    consensusctx "github.com/luxfi/consensus/context"
    "github.com/luxfi/node/consensus/adapter"
    // ... other imports
)

type addressManager struct {
    ctx      *consensusctx.Context
    bcLookup BCLookup
}

func NewAddressManager(ctx *consensusctx.Context) AddressManager {
    // Create context with BCLookup support
    extCtx := adapter.NewContextWithBCLookup(ctx)
    
    return &addressManager{
        ctx:      ctx,
        bcLookup: extCtx.GetBCLookup(),
    }
}

func (a *addressManager) ParseLocalAddress(addrStr string) (ids.ShortID, error) {
    chainID, addr, err := a.ParseAddress(addrStr)
    if err != nil {
        return ids.ShortID{}, err
    }
    
    expectedChainID := adapter.GetChainID(a.ctx)
    if chainID != expectedChainID {
        return ids.ShortID{}, fmt.Errorf(
            "%w: expected %q but got %q",
            ErrMismatchedChainIDs,
            expectedChainID,
            chainID,
        )
    }
    return addr, nil
}
```

#### 3.2 Update API Server

**File**: `/node/api/server/server.go` (modifications)

```go
func (s *server) RegisterChain(chainName string, ctx *consensuscontext.Context, vm core.VM) {
    s.lock.Lock()
    defer s.lock.Unlock()
    
    // Wrap VM with adapter if needed
    var adaptedVM core.VM
    if _, ok := vm.(interface{ NewHTTPHandler(context.Context) (http.Handler, error) }); !ok {
        adaptedVM = adapter.NewVMAdapter(vm)
    } else {
        adaptedVM = vm
    }
    
    // Register with adapted VM
    s.registerChainWithVM(chainName, ctx, adaptedVM)
}
```

### Deliverables
- [ ] Address manager using adapters
- [ ] API server handling adapted VMs
- [ ] Warp validator using adapted state
- [ ] Chain block wrapper fixed

## Phase 4: Utility and Math Functions (Days 7-8)

### Objective
Fix missing utility functions and math operations.

### Tasks

#### 4.1 Math Operations Bridge

**File**: `/node/consensus/utils/math.go`

```go
package utils

import (
    "errors"
    
    luxmath "github.com/luxfi/math"
)

var (
    ErrOverflow = errors.New("math overflow")
)

// Add safely adds two uint64 values
func Add(a, b uint64) (uint64, error) {
    return luxmath.Add64(a, b)
}

// Mul safely multiplies two uint64 values
func Mul(a, b uint64) (uint64, error) {
    return luxmath.Mul64(a, b)
}
```

#### 4.2 Update Import Statements

Fix duplicate imports and ensure proper aliasing throughout the codebase.

### Deliverables
- [ ] Math utilities implemented
- [ ] Import conflicts resolved
- [ ] Metric interfaces aligned
- [ ] All utility functions available

## Phase 5: Testing and Validation (Days 9-10)

### Objective
Comprehensive testing of the integration.

### Test Plan

#### 5.1 Unit Tests
```bash
# Test adapters
go test ./consensus/adapter/...

# Test updated components
go test ./vms/components/lux/...
go test ./api/server/...
go test ./vms/platformvm/warp/...
```

#### 5.2 Integration Tests
- VM registration and handler creation
- Context propagation through layers
- Validator state queries
- Address parsing with BCLookup

#### 5.3 Build Verification
```bash
# Full build
cd /Users/z/work/lux/node
go build ./...

# Run specific tests
go test -v ./consensus/adapter/...
```

### Deliverables
- [ ] All unit tests passing
- [ ] Integration tests implemented
- [ ] Build succeeds without errors
- [ ] Performance benchmarks acceptable

## Phase 6: Documentation and Cleanup (Days 11-12)

### Objective
Document the integration and clean up temporary code.

### Tasks

#### 6.1 Documentation
- Update package documentation
- Add adapter usage examples
- Document migration guide for VM developers

#### 6.2 Code Cleanup
- Remove temporary shims
- Consolidate duplicate code
- Optimize adapter implementations

### Deliverables
- [ ] Complete API documentation
- [ ] Migration guide for developers
- [ ] Code review completed
- [ ] Final implementation ready

## Risk Management

### High-Risk Areas

1. **Type Safety**
   - Risk: Runtime type assertion failures
   - Mitigation: Compile-time interface checks, comprehensive testing

2. **Performance**
   - Risk: Adapter overhead impacts performance
   - Mitigation: Benchmark critical paths, optimize hot paths

3. **Backward Compatibility**
   - Risk: Breaking existing VM implementations
   - Mitigation: Compatibility layer, gradual migration

### Contingency Plans

1. **If adapters cause performance issues**:
   - Implement caching in adapters
   - Use direct pass-through for critical operations
   - Consider code generation for adapters

2. **If interface mismatches persist**:
   - Create multiple adapter versions
   - Implement interface detection and auto-selection
   - Provide migration tools

## Success Criteria

### Build Success
- [ ] `go build ./...` completes without errors
- [ ] No undefined symbols or type mismatches

### Test Success
- [ ] All existing tests pass
- [ ] New adapter tests provide >80% coverage
- [ ] Integration tests verify end-to-end functionality

### Performance Criteria
- [ ] No more than 5% performance degradation
- [ ] Memory usage remains stable
- [ ] Adapter overhead < 1ms per operation

## Timeline Summary

| Phase | Duration | Deliverable |
|-------|----------|-------------|
| Phase 1: Foundation | 2 days | Adapter package, basic fixes |
| Phase 2: Interface Adapters | 2 days | VM and ValidatorState adapters |
| Phase 3: Component Integration | 2 days | Updated components using adapters |
| Phase 4: Utilities | 2 days | Math and utility functions |
| Phase 5: Testing | 2 days | Comprehensive test suite |
| Phase 6: Documentation | 2 days | Complete documentation |
| **Total** | **12 days** | **Full integration complete** |

## Next Immediate Steps

1. **Day 1 Morning**: Create adapter package structure
2. **Day 1 Afternoon**: Implement context adapter and helpers
3. **Day 2 Morning**: Fix addresses.go compilation errors
4. **Day 2 Afternoon**: Begin VM adapter implementation

## Command Checklist

```bash
# Create structure
mkdir -p /Users/z/work/lux/node/consensus/adapter

# Initial build test
cd /Users/z/work/lux/node
go build ./vms/components/lux/addresses.go 2>&1 | head -20

# After context adapter
go build ./consensus/adapter/...

# Test specific component
go test -v ./vms/components/lux/...

# Full build verification
go build ./...
```

## Conclusion

This implementation plan provides a systematic approach to integrating the consensus package with the node. By following this phased approach, we minimize risk while ensuring comprehensive integration. Each phase builds on the previous one, with clear deliverables and success criteria.

The adapter pattern provides flexibility for future changes while maintaining clean separation between packages. Regular testing at each phase ensures early detection of issues and maintains system stability throughout the integration process.