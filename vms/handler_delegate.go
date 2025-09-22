// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package vms provides a clean, generic handler delegation system for VM wrappers.
// This follows Go's philosophy of simple, composable interfaces with clear semantics.
package vms

import (
	"context"
	"net/http"
)

// HandlerProvider defines the interface for VMs that can provide HTTP handlers.
// This is the single source of truth for handler-providing VMs.
type HandlerProvider interface {
	CreateHandlers(context.Context) (map[string]http.Handler, error)
}

// StaticHandlerProvider extends HandlerProvider with static handlers.
type StaticHandlerProvider interface {
	HandlerProvider
	CreateStaticHandlers(context.Context) (map[string]http.Handler, error)
}

// HandlerDelegator provides a generic way to delegate handler creation
// to an underlying VM if it supports the HandlerProvider interface.
// This eliminates duplicate type checking code across VM wrappers.
type HandlerDelegator[T any] struct {
	vm T
}

// NewHandlerDelegator creates a new handler delegator for any VM type.
// Simple factory pattern - no complexity, just wrap and go.
func NewHandlerDelegator[T any](vm T) *HandlerDelegator[T] {
	return &HandlerDelegator[T]{vm: vm}
}

// CreateHandlers delegates to the underlying VM if it implements HandlerProvider.
// Returns empty map if not supported - fail gracefully, never panic.
func (d *HandlerDelegator[T]) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	if provider, ok := any(d.vm).(HandlerProvider); ok {
		return provider.CreateHandlers(ctx)
	}
	return make(map[string]http.Handler), nil
}

// CreateStaticHandlers delegates to the underlying VM if it implements StaticHandlerProvider.
// Returns empty map if not supported - consistent with CreateHandlers behavior.
func (d *HandlerDelegator[T]) CreateStaticHandlers(ctx context.Context) (map[string]http.Handler, error) {
	if provider, ok := any(d.vm).(StaticHandlerProvider); ok {
		return provider.CreateStaticHandlers(ctx)
	}
	return make(map[string]http.Handler), nil
}

// VM returns the underlying VM for direct access when needed.
// No hiding - transparency is a virtue.
func (d *HandlerDelegator[T]) VM() T {
	return d.vm
}

// DelegateHandlers is a helper function that checks if a VM implements
// HandlerProvider and calls CreateHandlers if it does.
// This is for when you don't need a full delegator, just a one-off check.
func DelegateHandlers(ctx context.Context, vm any) (map[string]http.Handler, error) {
	if provider, ok := vm.(HandlerProvider); ok {
		return provider.CreateHandlers(ctx)
	}
	return make(map[string]http.Handler), nil
}

// DelegateStaticHandlers is a helper function that checks if a VM implements
// StaticHandlerProvider and calls CreateStaticHandlers if it does.
func DelegateStaticHandlers(ctx context.Context, vm any) (map[string]http.Handler, error) {
	if provider, ok := vm.(StaticHandlerProvider); ok {
		return provider.CreateStaticHandlers(ctx)
	}
	return make(map[string]http.Handler), nil
}