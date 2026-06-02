//go:build !nattraversal

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"net/netip"
	"time"
)

// getUPnPRouter returns nil in minimal build (no UPnP support).
// Build with -tags=nattraversal for UPnP NAT traversal.
func getUPnPRouter() *upnpRouter {
	return nil
}

// upnpRouter stub for minimal build
type upnpRouter struct{}

func (*upnpRouter) SupportsNAT() bool                                    { return false }
func (*upnpRouter) MapPort(_, _ uint16, _ string, _ time.Duration) error { return nil }
func (*upnpRouter) UnmapPort(_, _ uint16) error                          { return nil }
func (*upnpRouter) ExternalIP() (netip.Addr, error)                      { return netip.Addr{}, nil }
