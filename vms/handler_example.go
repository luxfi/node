// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vms

import (
	"context"
	"fmt"
	"net/http"
)

// Example demonstrates the clean, DRY handler delegation pattern.
// This shows how any VM wrapper can use our generic solution.

// ExampleCChainVM represents a C-Chain VM that provides RPC handlers
type ExampleCChainVM struct {
	rpcServer *http.Server
}

// CreateHandlers returns the C-Chain's RPC handlers
func (vm *ExampleCChainVM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	// In real implementation, this would set up eth_*, web3_*, net_* endpoints
	handlers := make(map[string]http.Handler)
	handlers["/rpc"] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
	})
	handlers["/ws"] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// WebSocket handler for real-time subscriptions
	})
	return handlers, nil
}

// ExampleVMWrapper demonstrates how any wrapper can use HandlerDelegator
// This could be metervm, proposervm, or any custom wrapper.
type ExampleVMWrapper struct {
	innerVM interface{} // Could be any VM type
	*HandlerDelegator[interface{}]

	// Additional wrapper-specific fields
	metricsEnabled bool
	proposerMode   bool
}

// NewExampleVMWrapper shows the clean factory pattern
func NewExampleVMWrapper(innerVM interface{}) *ExampleVMWrapper {
	return &ExampleVMWrapper{
		innerVM:          innerVM,
		HandlerDelegator: NewHandlerDelegator(innerVM),
		metricsEnabled:   true,
		proposerMode:     false,
	}
}

// Example usage showing the beauty of composition:
// CreateHandlers is automatically inherited from HandlerDelegator.
// No need to write any delegation code!

// BuildBlock shows wrapper can focus on its core responsibility
func (w *ExampleVMWrapper) BuildBlock(ctx context.Context) error {
	// Wrapper logic here (metrics, proposer logic, etc.)

	// Delegate to inner VM if it has BuildBlock
	if builder, ok := w.innerVM.(interface{ BuildBlock(context.Context) error }); ok {
		return builder.BuildBlock(ctx)
	}
	return fmt.Errorf("inner VM doesn't support BuildBlock")
}

// ExampleUsage demonstrates the pattern in action
func ExampleUsage() {
	ctx := context.Background()

	// Create a C-Chain VM
	cchainVM := &ExampleCChainVM{}

	// Wrap it with any wrapper - handlers automatically work!
	wrapper1 := NewExampleVMWrapper(cchainVM)

	// Get handlers through the wrapper - clean delegation
	handlers, _ := wrapper1.CreateHandlers(ctx)
	fmt.Printf("Wrapper delegated %d handlers\n", len(handlers))

	// Even nested wrappers work perfectly
	wrapper2 := NewExampleVMWrapper(wrapper1)
	handlers2, _ := wrapper2.CreateHandlers(ctx)
	fmt.Printf("Double-wrapped still delegates %d handlers\n", len(handlers2))

	// VMs without handlers gracefully return empty map
	plainVM := struct{}{}
	wrapper3 := NewExampleVMWrapper(plainVM)
	handlers3, _ := wrapper3.CreateHandlers(ctx)
	fmt.Printf("Plain VM wrapper returns %d handlers\n", len(handlers3))
}

/*
Design Principles Demonstrated:

1. SIMPLICITY FIRST
   - Single generic type handles all delegation
   - No duplicate code across wrappers
   - Clear, obvious behavior

2. EXACTLY ONE WAY
   - All wrappers use HandlerDelegator
   - Consistent pattern everywhere
   - No alternative approaches needed

3. COMPOSITION OVER INHERITANCE
   - Embed HandlerDelegator for automatic functionality
   - Wrappers focus on their core purpose
   - Clean separation of concerns

4. FAIL GRACEFULLY
   - Non-handler VMs return empty maps
   - No panics, no complex error handling
   - Predictable behavior

5. TRANSPARENCY
   - VM() method provides direct access
   - No hidden magic
   - Easy to understand and debug

This is Go at its best: simple, powerful, and pedagogically clear.
Like Python's clarity with Go's type safety and performance.
*/