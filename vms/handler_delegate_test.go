// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vms

import (
	"context"
	"net/http"
	"testing"
)

// mockVM is a test VM that implements HandlerProvider
type mockVM struct {
	handlers       map[string]http.Handler
	staticHandlers map[string]http.Handler
}

func (m *mockVM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	return m.handlers, nil
}

func (m *mockVM) CreateStaticHandlers(ctx context.Context) (map[string]http.Handler, error) {
	return m.staticHandlers, nil
}

// plainVM is a test VM that doesn't implement HandlerProvider
type plainVM struct{}

func TestHandlerDelegator(t *testing.T) {
	ctx := context.Background()

	// Test with VM that implements HandlerProvider
	t.Run("VM with handlers", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		mockHandlers := map[string]http.Handler{
			"/api": handler,
			"/rpc": handler,
		}

		vm := &mockVM{
			handlers: mockHandlers,
		}

		delegator := NewHandlerDelegator(vm)
		handlers, err := delegator.CreateHandlers(ctx)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(handlers) != 2 {
			t.Fatalf("expected 2 handlers, got %d", len(handlers))
		}

		if handlers["/api"] == nil || handlers["/rpc"] == nil {
			t.Fatal("expected handlers to be present")
		}
	})

	// Test with VM that doesn't implement HandlerProvider
	t.Run("VM without handlers", func(t *testing.T) {
		vm := &plainVM{}
		delegator := NewHandlerDelegator(vm)
		handlers, err := delegator.CreateHandlers(ctx)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(handlers) != 0 {
			t.Fatalf("expected 0 handlers, got %d", len(handlers))
		}
	})

	// Test DelegateHandlers helper function
	t.Run("DelegateHandlers helper", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Test with HandlerProvider VM
		vm1 := &mockVM{
			handlers: map[string]http.Handler{"/test": handler},
		}
		handlers1, err := DelegateHandlers(ctx, vm1)
		if err != nil || len(handlers1) != 1 {
			t.Fatal("DelegateHandlers failed with HandlerProvider VM")
		}

		// Test with plain VM
		vm2 := &plainVM{}
		handlers2, err := DelegateHandlers(ctx, vm2)
		if err != nil || len(handlers2) != 0 {
			t.Fatal("DelegateHandlers failed with plain VM")
		}
	})
}