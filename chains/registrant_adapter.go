// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"github.com/luxfi/node/api/server"
	consensus "github.com/luxfi/consensus/context"
	"github.com/luxfi/consensus/core"
)

// registrantAdapter adapts a Server to implement chains.Registrant
type registrantAdapter struct {
	server server.Server
}

// NewRegistrantAdapter creates an adapter that allows Server to be used as chains.Registrant
func NewRegistrantAdapter(s server.Server) Registrant {
	return &registrantAdapter{server: s}
}

func (r *registrantAdapter) RegisterChain(chainName string, ctx *consensus.Context, vm core.VM) {
	r.server.RegisterChain(chainName, ctx, vm)
}
