// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
)

// Engine describes the standard interface of a consensus engine
type Engine interface {
	// Start the engine with the given request ID for bootstrapping
	Start(ctx context.Context, startReqID uint32) error

	// Shutdown the engine
	Shutdown(ctx context.Context) error
}

// VM describes the interface of a virtual machine
type VM interface {
	// Shutdown the VM
	Shutdown(context.Context) error
}