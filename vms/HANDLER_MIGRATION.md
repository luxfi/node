# Handler Delegation Migration Guide

## The Problem
Previously, every VM wrapper (chainVMWrapper, metervm, proposervm) had duplicate code checking if the underlying VM supports CreateHandlers:

```go
// OLD WAY - Duplicate code everywhere
func (vm *SomeWrapper) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
    if handlerVM, ok := vm.innerVM.(interface {
        CreateHandlers(context.Context) (map[string]http.Handler, error)
    }); ok {
        return handlerVM.CreateHandlers(ctx)
    }
    return make(map[string]http.Handler), nil
}
```

## The Solution
A clean, generic HandlerDelegator that eliminates all duplicate code:

```go
// NEW WAY - Clean composition
type MyWrapper struct {
    vm SomeVM
    *vms.HandlerDelegator[SomeVM]
}

func NewMyWrapper(vm SomeVM) *MyWrapper {
    return &MyWrapper{
        vm:               vm,
        HandlerDelegator: vms.NewHandlerDelegator(vm),
    }
}
// CreateHandlers is automatically inherited - no code needed!
```

## Migration Steps

### 1. For Simple Delegation
If you just need to check and delegate once:

```go
// Use the helper function
func (vm *MyVM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
    return vms.DelegateHandlers(ctx, vm.innerVM)
}
```

### 2. For VM Wrappers
Embed the HandlerDelegator:

```go
type MyWrapper struct {
    innerVM SomeVM
    *vms.HandlerDelegator[SomeVM]
}

func NewMyWrapper(vm SomeVM) *MyWrapper {
    return &MyWrapper{
        innerVM:          vm,
        HandlerDelegator: vms.NewHandlerDelegator(vm),
    }
}
```

### 3. Remove Old Code
Delete the old CreateHandlers and CreateStaticHandlers methods - they're inherited now!

## Benefits

1. **DRY** - No duplicate type checking code
2. **Type Safe** - Go generics ensure compile-time safety
3. **Composable** - Works with nested wrappers
4. **Simple** - One pattern for all VMs
5. **Maintainable** - Changes in one place affect all wrappers

## Design Philosophy

This follows Go's core principles:
- **Simplicity**: One obvious way to do things
- **Composition**: Embed functionality, don't inherit
- **Clarity**: Code is pedagogically clear
- **Robustness**: Fail gracefully with empty maps

## Complete Example

```go
// Your VM implements block.ChainVM
type MyBlockVM struct {
    // VM fields
}

// Wrapper with clean handler delegation
type MyVMWrapper struct {
    vm *MyBlockVM
    *vms.HandlerDelegator[*MyBlockVM]

    // Your wrapper-specific fields
    metricsCollector *metrics.Collector
}

// Factory function
func WrapVM(vm *MyBlockVM) *MyVMWrapper {
    return &MyVMWrapper{
        vm:               vm,
        HandlerDelegator: vms.NewHandlerDelegator(vm),
        metricsCollector: metrics.NewCollector(),
    }
}

// Focus on your wrapper's core logic
func (w *MyVMWrapper) BuildBlock(ctx context.Context) (Block, error) {
    start := time.Now()
    defer w.metricsCollector.RecordBlockBuild(time.Since(start))

    return w.vm.BuildBlock(ctx)
}

// CreateHandlers works automatically through composition!
```

## Testing

```go
func TestMyWrapperHandlers(t *testing.T) {
    vm := &MyBlockVM{}
    wrapper := WrapVM(vm)

    handlers, err := wrapper.CreateHandlers(context.Background())
    require.NoError(t, err)

    // If VM implements handlers, they're returned
    // If not, empty map is returned
    assert.NotNil(t, handlers)
}
```

This is the Go way: simple, clean, and beautiful.