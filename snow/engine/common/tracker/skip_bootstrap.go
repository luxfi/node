// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"context"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/version"
)

var _ Startup = (*skipBootstrap)(nil)

// skipBootstrap is a Startup tracker that always returns true for ShouldStart
// to skip bootstrapping phase
type skipBootstrap struct {
	Peers
}

func NewSkipBootstrap(peers Peers) Startup {
	return &skipBootstrap{
		Peers: peers,
	}
}

func (s *skipBootstrap) ShouldStart() bool {
	return true
}

func (s *skipBootstrap) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	return s.Peers.Connected(ctx, nodeID, nodeVersion)
}