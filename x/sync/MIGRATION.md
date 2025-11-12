# Migration Guide: x/sync Generics

This guide helps you migrate from the non-generic x/sync package to the new generic version (v1.11.0+).

## Overview

The x/sync package now uses Go generics to provide better type safety and flexibility. The migration is designed to be backward-compatible, with existing code continuing to work without changes.

## Key Changes

### 1. Generic Type Parameters

The package now uses three generic type parameters:

- **`T`**: Database response type (must implement `merkledb.MerkleRootGetter`)
- **`U`**: Range proof type
- **`V`**: Change proof type

### 2. Interface Updates

#### Before (Non-Generic)
```go
type Client interface {
    GetRangeProof(ctx context.Context, request *pb.SyncGetRangeProofRequest) (*merkledb.RangeProof, error)
    GetChangeProof(ctx context.Context, request *pb.SyncGetChangeProofRequest, responseType ResponseType) (*merkledb.ChangeOrRangeProof, error)
}
```

#### After (Generic)
```go
type Client[T merkledb.MerkleRootGetter, U any, V any] interface {
    GetRangeProof(ctx context.Context, request *pb.SyncGetRangeProofRequest) (U, error)
    GetChangeProof(ctx context.Context, request *pb.SyncGetChangeProofRequest, responseType ResponseType) (V, error)
}
```

## Migration Steps

### Step 1: Update Client Creation

#### Before
```go
client := sync.NewClient(
    config,
    log,
    metrics,
    db,
    requestHandler,
)
```

#### After (Explicit Types)
```go
client := sync.NewClient[*merkledb.Database, *merkledb.RangeProof, *merkledb.ChangeOrRangeProof](
    config,
    log,
    metrics,
    db,
    requestHandler,
)
```

#### After (Type Inference - Recommended)
```go
// Go will infer types from the parameters
client := sync.NewClient(
    config,
    log,
    metrics,
    db,
    requestHandler,
)
```

### Step 2: Update Manager Creation

#### Before
```go
manager := sync.NewManager(
    config,
    db,
    client,
    log,
    targetFn,
)
```

#### After (Explicit Types)
```go
manager := sync.NewManager[*merkledb.Database](
    config,
    db,
    client,
    log,
    targetFn,
)
```

#### After (Type Inference - Recommended)
```go
// Type inferred from targetFn return type
manager := sync.NewManager(
    config,
    db,
    client,
    log,
    targetFn,
)
```

### Step 3: Update Custom Implementations

If you have custom implementations of the sync interfaces:

#### Custom Client Implementation

##### Before
```go
type MyClient struct {
    // fields
}

func (c *MyClient) GetRangeProof(ctx context.Context, request *pb.SyncGetRangeProofRequest) (*merkledb.RangeProof, error) {
    // implementation
}
```

##### After
```go
type MyClient[T merkledb.MerkleRootGetter, U any, V any] struct {
    // fields
}

func (c *MyClient[T, U, V]) GetRangeProof(ctx context.Context, request *pb.SyncGetRangeProofRequest) (U, error) {
    // implementation
}
```

#### Custom Database Response Type

##### Creating a Custom Type
```go
type MyDBResponse struct {
    root     ids.ID
    height   uint64
    metadata map[string]interface{}
}

// Must implement MerkleRootGetter
func (m *MyDBResponse) GetMerkleRoot() ids.ID {
    return m.root
}

// Use with sync package
client := sync.NewClient[*MyDBResponse, *merkledb.RangeProof, *merkledb.ChangeOrRangeProof](
    config,
    log,
    metrics,
    db,
    requestHandler,
)
```

### Step 4: Update Mock Implementations

#### Before
```go
mockClient := &sync.MockClient{}
mockClient.On("GetRangeProof", mock.Anything, mock.Anything).Return(&merkledb.RangeProof{}, nil)
```

#### After
```go
mockClient := &sync.MockClient[*merkledb.Database, *merkledb.RangeProof, *merkledb.ChangeOrRangeProof]{}
mockClient.On("GetRangeProof", mock.Anything, mock.Anything).Return(&merkledb.RangeProof{}, nil)
```

## Common Patterns

### Pattern 1: Using Standard Types

Most users can continue using the standard types without changes:

```go
// The package provides sensible defaults
client := sync.NewClient(config, log, metrics, db, requestHandler)
manager := sync.NewManager(config, db, client, log, targetFn)
```

### Pattern 2: Custom Proof Types

If you need custom proof types:

```go
type MyRangeProof struct {
    *merkledb.RangeProof
    CustomField string
}

type MyChangeProof struct {
    *merkledb.ChangeOrRangeProof
    Timestamp   time.Time
}

client := sync.NewClient[*merkledb.Database, *MyRangeProof, *MyChangeProof](
    config,
    log,
    metrics,
    db,
    customRequestHandler,
)
```

### Pattern 3: Working with Multiple Database Types

```go
// For primary database
primaryClient := sync.NewClient[*PrimaryDB, *merkledb.RangeProof, *merkledb.ChangeOrRangeProof](
    primaryConfig, log, metrics, primaryDB, primaryHandler,
)

// For secondary database
secondaryClient := sync.NewClient[*SecondaryDB, *merkledb.RangeProof, *merkledb.ChangeOrRangeProof](
    secondaryConfig, log, metrics, secondaryDB, secondaryHandler,
)
```

## Testing

### Unit Tests

The generic implementation maintains full test compatibility:

```go
func TestSyncClient(t *testing.T) {
    // Existing tests work without changes
    client := sync.NewClient(config, log, metrics, db, handler)
    
    // Test as before
    proof, err := client.GetRangeProof(ctx, request)
    require.NoError(t, err)
    require.NotNil(t, proof)
}
```

### Integration Tests

```go
func TestSyncIntegration(t *testing.T) {
    // Generic types provide better compile-time safety
    var client sync.Client[*merkledb.Database, *merkledb.RangeProof, *merkledb.ChangeOrRangeProof]
    
    client = sync.NewClient(config, log, metrics, db, handler)
    
    // Type safety prevents incorrect usage at compile time
    // This would fail at compile time if types don't match
    manager := sync.NewManager(config, db, client, log, targetFn)
}
```

## Troubleshooting

### Issue: Type Inference Failures

**Problem**: Go can't infer types automatically

**Solution**: Explicitly specify types
```go
// Instead of
client := sync.NewClient(config, log, metrics, db, handler)

// Use
client := sync.NewClient[*merkledb.Database, *merkledb.RangeProof, *merkledb.ChangeOrRangeProof](
    config, log, metrics, db, handler,
)
```

### Issue: Interface Compatibility

**Problem**: Custom implementation doesn't match new interface

**Solution**: Update method signatures to use generic types
```go
// Update from
func (c *MyClient) GetRangeProof(...) (*merkledb.RangeProof, error)

// To
func (c *MyClient[T, U, V]) GetRangeProof(...) (U, error)
```

### Issue: Mock Testing

**Problem**: Mocks need type parameters

**Solution**: Use concrete types for mocks
```go
type MockClient = sync.MockClient[*merkledb.Database, *merkledb.RangeProof, *merkledb.ChangeOrRangeProof]

mock := &MockClient{}
```

## Best Practices

1. **Use Type Inference**: Let Go infer types when possible for cleaner code
2. **Consistent Types**: Use the same type parameters across related components
3. **Document Custom Types**: Clearly document any custom types that implement required interfaces
4. **Test Thoroughly**: Ensure all tests pass with the new generic implementation
5. **Gradual Migration**: Migrate one component at a time if you have a large codebase

## Performance Notes

The generic implementation has **zero performance overhead**:

- Generic instantiation happens at compile time
- No runtime type assertions or boxing
- Identical memory layout and CPU usage
- Better inlining opportunities with concrete types

## Version Compatibility

- **v1.10.x and earlier**: Non-generic implementation
- **v1.11.0+**: Generic implementation with full backward compatibility
- **Migration**: No breaking changes for standard usage

## Getting Help

If you encounter issues during migration:

1. Check that all type parameters match across components
2. Ensure custom types implement required interfaces
3. Review the test files for examples of correct usage
4. Use explicit type parameters if inference fails

## Summary

The migration to generics in x/sync provides:

- ✅ Better type safety at compile time
- ✅ Full backward compatibility
- ✅ Zero performance overhead
- ✅ More flexible custom implementations
- ✅ Cleaner, more maintainable code

Most users can continue using the package without any code changes, while advanced users gain the ability to use custom types with full type safety.