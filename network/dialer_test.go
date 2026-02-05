// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"

	"github.com/luxfi/net/endpoints"
	"github.com/luxfi/node/network/dialer"
)

var (
	errRefused = errors.New("connection refused")

	_ dialer.EndpointDialer = (*testDialer)(nil)
)

type testDialer struct {
	// maps [ip.String] to a listener
	listeners map[netip.AddrPort]*testListener
	// hostname resolution map for testing hostname endpoints
	// maps hostname:port to resolved netip.AddrPort
	resolve map[string]netip.AddrPort
}

func newTestDialer() *testDialer {
	return &testDialer{
		listeners: make(map[netip.AddrPort]*testListener),
		resolve:   make(map[string]netip.AddrPort),
	}
}

// AddHostnameResolution adds a hostname -> IP resolution for testing
func (d *testDialer) AddHostnameResolution(hostname string, port uint16, addr netip.AddrPort) {
	key := fmt.Sprintf("%s:%d", hostname, port)
	d.resolve[key] = addr
}

func (d *testDialer) NewListener() (netip.AddrPort, *testListener) {
	// Uses a private IP to easily enable testing AllowPrivateIPs
	addrPort := netip.AddrPortFrom(
		netip.AddrFrom4([4]byte{10, 0, 0, 0}),
		uint16(len(d.listeners)+1),
	)
	listener := newTestListener(addrPort)
	d.AddListener(addrPort, listener)
	return addrPort, listener
}

func (d *testDialer) AddListener(ip netip.AddrPort, listener *testListener) {
	d.listeners[ip] = listener
}

func (d *testDialer) Dial(ctx context.Context, ip netip.AddrPort) (net.Conn, error) {
	listener, ok := d.listeners[ip]
	if !ok {
		return nil, errRefused
	}
	serverConn, clientConn := net.Pipe()
	server := &testConn{
		Conn: serverConn,
		localAddr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 1,
		},
		remoteAddr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 2,
		},
	}
	client := &testConn{
		Conn: clientConn,
		localAddr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 3,
		},
		remoteAddr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 4,
		},
	}
	select {
	case listener.inbound <- server:
		return client, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-listener.closed:
		return nil, errRefused
	}
}

// DialEndpoint implements dialer.EndpointDialer for testing.
// For hostname endpoints, it uses the resolve map to look up the IP.
func (d *testDialer) DialEndpoint(ctx context.Context, endpoint endpoints.Endpoint) (net.Conn, error) {
	if endpoint.IsIP() {
		return d.Dial(ctx, endpoint.AddrPort)
	}

	// Hostname endpoint - look up in resolve map
	key := fmt.Sprintf("%s:%d", endpoint.Hostname, endpoint.Port)
	resolved, ok := d.resolve[key]
	if !ok {
		return nil, errRefused
	}
	return d.Dial(ctx, resolved)
}
