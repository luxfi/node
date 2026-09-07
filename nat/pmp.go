//go:build !nattraversal

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"net/netip"
	"time"
)

// getPMPRouter returns nil in minimal build (no NAT-PMP support).
// Build with -tags=nattraversal for NAT-PMP support.
func getPMPRouter() *pmpRouter {
	return nil
}

// pmpRouter stub for minimal build
type pmpRouter struct{}

func (*pmpRouter) SupportsNAT() bool                                    { return false }
func (*pmpRouter) MapPort(_, _ uint16, _ string, _ time.Duration) error { return nil }
func (*pmpRouter) UnmapPort(_, _ uint16) error                          { return nil }
func (*pmpRouter) ExternalIP() (netip.Addr, error)                      { return netip.Addr{}, nil }
