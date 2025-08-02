// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

// ContextInitializable defines the interface for types that need consensus context initialization
type ContextInitializable interface {
	// Initialize initializes with the consensus context
	Initialize(ctx *Context) error
}