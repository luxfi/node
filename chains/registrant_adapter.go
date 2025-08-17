// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"

	"github.com/luxfi/consensus/core"
	"github.com/luxfi/node/api/server"
)

// registrantAdapter adapts a Server to implement chains.Registrant
type registrantAdapter struct {
	server server.Server
}

// NewRegistrantAdapter creates an adapter that allows Server to be used as chains.Registrant
func NewRegistrantAdapter(s server.Server) Registrant {
	return &registrantAdapter{server: s}
}

func (r *registrantAdapter) RegisterChain(chainName string, ctx context.Context, vm interface{}) {
	// Try to cast vm to core.VM, if it fails, we can't register it
	if coreVM, ok := vm.(core.VM); ok {
		r.server.RegisterChain(chainName, ctx, coreVM)
	}
	// If it's not a core.VM, we silently skip registration
	// This handles other VM types like vertex.LinearizableVMWithEngine
}