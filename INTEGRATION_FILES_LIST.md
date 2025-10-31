# Consensus Integration - Files Requiring Changes

## Priority 1: Critical Build Errors (Must Fix First)

### 1. `/node/vms/components/lux/addresses.go`
**Current Issues:**
- Line 53: Unknown field `bcLookup` in struct literal
- Line 62, 86, 104: `undefined: consensus`

**Required Changes:**
```go
// Add imports
import "github.com/luxfi/node/consensus/adapter"

// Fix struct initialization (line 50-54)
func NewAddressManager(ctx *consensusctx.Context) AddressManager {
    return &addressManager{
        ctx: ctx,
        // Remove bcLookup field - will get from context adapter
    }
}

// Fix consensus references (line 62)
expectedChainID := adapter.GetChainID(a.ctx)

// Fix consensus references (line 86)
networkID := adapter.GetNetworkID(a.ctx)
```

### 2. `/node/api/server/server.go`
**Current Issues:**
- Line 161, 196: `log.Error(err)` used as value
- Line 166, 169: `undefined: chainID`
- VM missing `NewHTTPHandler` method

**Required Changes:**
```go
// Fix log errors (line 161, 196)
s.log.Error("error message", log.Error(err))

// Fix chainID references
chainID := ctx.ChainID

// Add VM adapter support in RegisterChain
```

### 3. `/node/vms/platformvm/warp/validator.go`
**Current Issues:**
- Line 71: `undefined: subnetID`
- Line 77: Type mismatch `ValidatorData` vs `GetValidatorOutput`
- Line 209: `ValidatorState` missing `GetNetID` method

**Required Changes:**
```go
// Fix subnetID (line 71)
vdrSet, err := pChainState.GetValidatorSet(ctx, pChainHeight, subsubnetID)

// Use adapter for GetNetID
import "github.com/luxfi/node/consensus/adapter"
// Wrap validator state with adapter
```

## Priority 2: Interface Mismatches

### 4. `/node/vms/components/chain/block.go`
**Current Issues:**
- Line 10: Wrong import path for consensus chain package
- Line 110, 113: `undefined: chain`

**Required Changes:**
```go
// Fix import
import "github.com/luxfi/consensus/engine/chain/block"

// Add proper namespace
```

### 5. `/node/vms/secp256k1fx/*.go` (Multiple Files)
**Current Issues:**
- Duplicate imports (keychain, reflect, log)
- Context type mismatches
- Math function undefined (Mul, Add)

**Required Changes:**
- Remove duplicate imports
- Use consensus context consistently
- Import math utilities from adapter

## Priority 3: Supporting Infrastructure

### 6. New Files to Create

#### `/node/consensus/adapter/context.go`
```go
package adapter

// Context adapter implementation
type ContextWithBCLookup struct {
    *consensusctx.Context
    bcLookup lux.BCLookup
}
```

#### `/node/consensus/adapter/helpers.go`
```go
package adapter

// Helper functions for consensus context
func GetChainID(ctx *consensusctx.Context) ids.ID
func GetNetworkID(ctx *consensusctx.Context) uint32
func GetNetID(ctx *consensusctx.Context) ids.ID
```

#### `/node/consensus/adapter/vm_adapter.go`
```go
package adapter

// VM adapter to bridge interface gaps
type VMAdapter struct {
    core.VM
}

func (v *VMAdapter) NewHTTPHandler(ctx context.Context) (http.Handler, error)
```

#### `/node/consensus/adapter/validator_adapter.go`
```go
package adapter

// ValidatorState adapter
type ValidatorStateAdapter struct {
    validators.State
}

func (v *ValidatorStateAdapter) GetNetID(ctx context.Context, chainID ids.ID) (ids.ID, error)
```

#### `/node/consensus/adapter/bclookup.go`
```go
package adapter

// BCLookup implementation
type BCLookupImpl struct {
    aliases map[ids.ID][]string
}

func (b *BCLookupImpl) Lookup(alias string) (ids.ID, error)
func (b *BCLookupImpl) PrimaryAlias(chainID ids.ID) (string, error)
```

## Priority 4: Utility Functions

### 7. `/node/consensus/utils/math.go` (New File)
```go
package utils

// Math utilities
func Add(a, b uint64) (uint64, error)
func Mul(a, b uint64) (uint64, error)
```

## Build Order

To fix the build in minimal steps:

1. **Step 1**: Create adapter package with helpers
   ```bash
   mkdir -p /Users/z/work/lux/node/consensus/adapter
   # Create helpers.go with GetChainID, GetNetworkID functions
   ```

2. **Step 2**: Fix addresses.go
   - Import adapter package
   - Replace undefined consensus references

3. **Step 3**: Create VM adapter
   - Implement NewHTTPHandler method
   - Update server.go to use adapter

4. **Step 4**: Create ValidatorState adapter
   - Implement GetNetID method
   - Update warp/validator.go

5. **Step 5**: Fix remaining imports and math functions

## Quick Test Commands

```bash
# Test individual file compilation
go build ./vms/components/lux/addresses.go

# Test adapter package
go build ./consensus/adapter/...

# Test full build
go build ./...

# Run tests
go test ./consensus/adapter/...
```

## File Count Summary

- **Files with errors**: ~15 files
- **New files to create**: 6 files
- **Critical files to fix first**: 3 files
- **Total lines to modify**: ~200 lines
- **New lines to write**: ~500 lines

## Success Metric

The build should complete successfully with:
```bash
cd /Users/z/work/lux/node
go build ./...
# No output = success
```