# Consensus Package Integration Architecture

## Executive Summary

The Lux node codebase has a fundamental architectural challenge: integrating a standalone consensus package (`/Users/z/work/lux/consensus`) with the node implementation (`/Users/z/work/lux/node`). This document outlines the integration issues, proposes architectural patterns, and provides an implementation plan.

## Current State Analysis

### Package Structure
- **Consensus Package**: `/Users/z/work/lux/consensus` - Standalone consensus implementation
  - Module: `github.com/luxfi/consensus`
  - Contains: Core consensus algorithms, contexts, interfaces, VMs
  - Dependencies: Uses luxfi packages (crypto, database, ids, log, etc.)
  
- **Node Package**: `/Users/z/work/lux/node` - Node implementation
  - Module: `github.com/luxfi/node`
  - Contains: VM implementations, chain management, API servers
  - Dependencies: Imports consensus package for context and core types

## Core Integration Issues

### 1. Context Type Mismatch
**Problem**: The node uses `consensuscontext.Context` from the consensus package, but there are interface mismatches.

**Issues Found**:
- `vms/components/lux/addresses.go`: Expects BCLookup interface methods (PrimaryAlias, Lookup) not present in consensus context
- Missing helper functions like `consensus.GetChainID()`, `consensus.GetNetworkID()`
- Context Lock field used in api/server but not properly integrated

### 2. VM Interface Incompatibility
**Problem**: The consensus package's VM interface differs from what the node expects.

**Issues Found**:
- `api/server/server.go`: Expects VM.NewHTTPHandler() method
- VM interface in consensus/core/vm.go has different methods than node expectations
- Missing CreateHandlers, CreateStaticHandlers methods

### 3. ValidatorState Interface Gaps
**Problem**: Different ValidatorState interfaces between packages.

**Issues Found**:
- `vms/platformvm/warp/validator.go`: Expects GetNetID method on ValidatorState
- Type mismatch between ValidatorData and GetValidatorOutput structures
- Missing conversion utilities

### 4. Missing Bridge Utilities
**Problem**: No bridge layer to translate between consensus and node types.

**Issues Found**:
- No BlockchainIDLookup implementation that satisfies both packages
- Missing chain package bridge (consensus/engine/chain vs node/vms/components/chain)
- No adapter for consensus.Context to provide node-required functionality

## Proposed Architecture

### Design Principles
1. **Adapter Pattern**: Create adapters to bridge between consensus and node interfaces
2. **Interface Segregation**: Define minimal interfaces for specific use cases
3. **Type Conversion Layer**: Explicit conversion utilities between package types
4. **Backward Compatibility**: Maintain existing APIs where possible

### Architectural Components

```
┌─────────────────────────────────────────────────────────────┐
│                         Node Package                         │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────┐  │
│  │     VMs     │  │  API Server  │  │  Chain Manager   │  │
│  └──────┬──────┘  └──────┬───────┘  └────────┬─────────┘  │
│         │                 │                    │            │
│  ┌──────▼─────────────────▼────────────────────▼────────┐  │
│  │              Bridge/Adapter Layer                     │  │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────────┐  │  │
│  │  │  Context   │  │     VM     │  │  Validator     │  │  │
│  │  │  Adapter   │  │  Adapter   │  │  State Adapter │  │  │
│  │  └────────────┘  └────────────┘  └────────────────┘  │  │
│  └───────────────────────┬───────────────────────────────┘  │
│                          │                                   │
└──────────────────────────┼───────────────────────────────────┘
                           │
┌──────────────────────────▼───────────────────────────────────┐
│                    Consensus Package                         │
│  ┌──────────────┐  ┌─────────────┐  ┌─────────────────┐    │
│  │   Context    │  │   Core VM   │  │  Validator      │    │
│  │  Definition  │  │  Interface  │  │  Interfaces     │    │
│  └──────────────┘  └─────────────┘  └─────────────────┘    │
└──────────────────────────────────────────────────────────────┘
```

## Implementation Components

### 1. Context Adapter (`/node/consensus/adapter/context.go`)

```go
package adapter

import (
    consensusctx "github.com/luxfi/consensus/context"
    "github.com/luxfi/ids"
    "github.com/luxfi/node/vms/components/lux"
)

// ContextAdapter bridges consensus.Context to node requirements
type ContextAdapter struct {
    *consensusctx.Context
    bcLookup lux.BCLookup
}

// NewContextAdapter creates a new context adapter
func NewContextAdapter(ctx *consensusctx.Context, bcLookup lux.BCLookup) *ContextAdapter {
    return &ContextAdapter{
        Context:  ctx,
        bcLookup: bcLookup,
    }
}

// GetBCLookup returns the blockchain lookup interface
func (c *ContextAdapter) GetBCLookup() lux.BCLookup {
    return c.bcLookup
}

// Helper functions for backward compatibility
func GetChainID(ctx *consensusctx.Context) ids.ID {
    return ctx.ChainID
}

func GetNetworkID(ctx *consensusctx.Context) uint32 {
    return ctx.QuantumID
}
```

### 2. VM Adapter (`/node/consensus/adapter/vm.go`)

```go
package adapter

import (
    "context"
    "net/http"
    
    "github.com/luxfi/consensus/core"
    consensusctx "github.com/luxfi/consensus/context"
)

// VMAdapter bridges consensus VM to node VM requirements
type VMAdapter struct {
    core.VM
    handlers map[string]http.Handler
}

// NewVMAdapter creates a new VM adapter
func NewVMAdapter(vm core.VM) *VMAdapter {
    return &VMAdapter{
        VM:       vm,
        handlers: make(map[string]http.Handler),
    }
}

// NewHTTPHandler returns HTTP handler for the VM
func (v *VMAdapter) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
    handlers, err := v.CreateHandlers(ctx)
    if err != nil {
        return nil, err
    }
    
    // Create multiplexed handler
    mux := http.NewServeMux()
    for path, handler := range handlers {
        mux.Handle(path, handler)
    }
    return mux, nil
}
```

### 3. ValidatorState Adapter (`/node/consensus/adapter/validator.go`)

```go
package adapter

import (
    "context"
    
    "github.com/luxfi/consensus/validators"
    "github.com/luxfi/ids"
    "github.com/luxfi/node/vms/platformvm/warp"
)

// ValidatorStateAdapter bridges consensus ValidatorState to node requirements
type ValidatorStateAdapter struct {
    validators.State
}

// GetNetID returns the network ID for a chain
func (v *ValidatorStateAdapter) GetNetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
    // Implementation to get net ID from chain ID
    return v.GetSubnetID(ctx, chainID)
}

// GetValidatorSet returns validator data in the expected format
func (v *ValidatorStateAdapter) GetValidatorSet(
    ctx context.Context,
    height uint64,
    subnetID ids.ID,
) (map[ids.NodeID]*warp.ValidatorData, error) {
    // Get validators from consensus layer
    vdrs, err := v.State.GetValidatorSet(ctx, height, subnetID)
    if err != nil {
        return nil, err
    }
    
    // Convert to warp.ValidatorData format
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

### 4. BCLookup Implementation (`/node/consensus/adapter/bclookup.go`)

```go
package adapter

import (
    "github.com/luxfi/ids"
    "github.com/luxfi/node/vms/components/lux"
)

// BCLookupImpl implements the BCLookup interface
type BCLookupImpl struct {
    aliases map[ids.ID][]string
    primary map[ids.ID]string
}

// NewBCLookup creates a new blockchain lookup implementation
func NewBCLookup() lux.BCLookup {
    return &BCLookupImpl{
        aliases: make(map[ids.ID][]string),
        primary: make(map[ids.ID]string),
    }
}

// Lookup returns the blockchain ID for an alias
func (b *BCLookupImpl) Lookup(alias string) (ids.ID, error) {
    // Implementation
}

// PrimaryAlias returns the primary alias for a blockchain
func (b *BCLookupImpl) PrimaryAlias(chainID ids.ID) (string, error) {
    // Implementation
}
```

## File Changes Required

### Phase 1: Create Adapter Layer
1. **New Directory**: `/node/consensus/adapter/`
   - `context.go` - Context adapter implementation
   - `vm.go` - VM adapter implementation
   - `validator.go` - ValidatorState adapter
   - `bclookup.go` - BCLookup implementation
   - `utils.go` - Helper functions and conversions

### Phase 2: Update Existing Files
1. **vms/components/lux/addresses.go**
   - Import adapter package
   - Use ContextAdapter instead of raw consensus.Context
   - Remove undefined consensus references

2. **api/server/server.go**
   - Use VMAdapter for VM registration
   - Update RegisterChain to handle adapted VMs

3. **vms/platformvm/warp/validator.go**
   - Use ValidatorStateAdapter
   - Update type conversions

4. **vms/components/chain/block.go**
   - Import proper chain package from consensus
   - Add type conversions where needed

### Phase 3: Integration Points
1. **Chain Manager Updates**
   - Update chain creation to use adapters
   - Ensure proper context propagation

2. **VM Factory Updates**
   - Wrap VM instances with VMAdapter
   - Register adapted VMs with server

## Migration Strategy

### Step 1: Implement Core Adapters
```bash
# Create adapter package
mkdir -p /Users/z/work/lux/node/consensus/adapter
# Implement core adapter files
```

### Step 2: Update Import Statements
```go
// Before
import consensusctx "github.com/luxfi/consensus/context"

// After
import (
    consensusctx "github.com/luxfi/consensus/context"
    "github.com/luxfi/node/consensus/adapter"
)
```

### Step 3: Progressive Migration
1. Start with least dependent components (addresses, utils)
2. Move to VM adapters
3. Finally update server and chain management

## Testing Strategy

### Unit Tests
- Test each adapter in isolation
- Verify interface compliance
- Test type conversions

### Integration Tests
- Test VM registration with adapted VMs
- Test context propagation through layers
- Test validator state queries

### Regression Tests
- Ensure existing functionality remains intact
- Test backward compatibility
- Performance benchmarks

## Risk Mitigation

### Identified Risks
1. **Type Safety**: Ensure proper type conversions
2. **Performance**: Minimize adapter overhead
3. **Compatibility**: Maintain API compatibility

### Mitigation Strategies
1. Use compile-time interface checks
2. Implement efficient pass-through for common operations
3. Provide compatibility shims for existing code

## Success Metrics

1. **Compilation**: All packages compile without errors
2. **Tests**: All existing tests pass
3. **Integration**: VMs can be registered and called
4. **Performance**: No significant performance degradation

## Next Steps

1. **Review**: Architecture review with team
2. **Prototype**: Implement minimal adapter for proof of concept
3. **Iterate**: Refine based on prototype findings
4. **Implement**: Full implementation following this design
5. **Test**: Comprehensive testing at each stage
6. **Deploy**: Gradual rollout with monitoring

## Appendix: Interface Definitions

### Required Interfaces

```go
// BCLookup interface required by node
type BCLookup interface {
    Lookup(string) (ids.ID, error)
    PrimaryAlias(ids.ID) (string, error)
}

// VM interface extensions needed
type VMExtensions interface {
    NewHTTPHandler(context.Context) (http.Handler, error)
    CreateHandlers(context.Context) (map[string]http.Handler, error)
    CreateStaticHandlers(context.Context) (map[string]http.Handler, error)
}

// ValidatorState extensions needed
type ValidatorStateExtensions interface {
    GetNetID(context.Context, ids.ID) (ids.ID, error)
}
```

## Conclusion

This architecture provides a clean separation between the consensus package and node implementation while maintaining compatibility. The adapter pattern allows for gradual migration and testing at each stage. The implementation should proceed in phases, with careful testing at each step to ensure system stability.