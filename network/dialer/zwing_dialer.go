// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Z-Wing PQ secure channel wrapper for the p2p dialer.
//
// Z-Wing (LP-9702) is the canonical Lux post-quantum secure channel:
// IETF X-Wing KEM (X25519 + ML-KEM-768) plus an Ed25519 + ML-DSA-65
// hybrid identity plus ChaCha20-Poly1305 with sequence-numbered
// nonces. This file wraps the regular `EndpointDialer` so that any
// `net.Conn` it produces — over plain TCP today and over RNS mesh
// transport once `RNSTransport` is wired in — gets a Z-Wing handshake
// applied on top.
//
// Architecture:
//
//	┌────────────────────────────────────────────────────────────┐
//	│ Application bytes (ZAP / p2p frames)                       │
//	├────────────────────────────────────────────────────────────┤
//	│ Z-Wing AEAD records (ChaCha20-Poly1305, seq-numbered)      │
//	├────────────────────────────────────────────────────────────┤
//	│ net.Conn — TCP today, RNS mesh link tomorrow               │
//	└────────────────────────────────────────────────────────────┘
//
// The same `ZWingDialer` works on either underlying transport —
// Z-Wing's contract is "any `net.Conn`". The replacement of legacy
// LP-9701 in-RNS-link crypto with Z-Wing happens above this file:
// callers route `endpointDialer.DialEndpoint(...)` through
// `ZWingDialer.Wrap(...)` and the secure channel is the same shape
// regardless of whether the underlying conn is IP, hostname, or RNS.

package dialer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/net/endpoints"
	"github.com/luxfi/zwing"
)

// ErrZWingMissingIdentity is returned when ZWingDialer is configured
// without a local Z-Wing identity.
var ErrZWingMissingIdentity = errors.New("zwing dialer: missing LocalIdentity")

// ZWingDialer wraps an underlying `EndpointDialer` so every produced
// `net.Conn` is upgraded with a Z-Wing 1-RTT mutual-auth handshake.
//
// The base dialer is responsible for picking the transport (plain TCP
// or RNS mesh link); the Z-Wing layer is identical in both cases —
// the secure channel rides byte-for-byte over whatever bytes the base
// transport delivers.
type ZWingDialer struct {
	base EndpointDialer
	cfg  *zwing.Config
	log  log.Logger
}

// ZWingDialerConfig configures a Z-Wing-secured dialer.
type ZWingDialerConfig struct {
	// LocalIdentity is this node's long-term Z-Wing identity (Ed25519 +
	// ML-DSA-65 hybrid + X-Wing static key). Required.
	LocalIdentity *zwing.Identity

	// HandshakeTimeout bounds the post-dial handshake. Zero means no
	// timeout.
	HandshakeTimeout time.Duration
}

// NewZWingDialer wraps `base` so each produced connection runs the
// Z-Wing handshake as the initiator. Returns an error if `cfg` is
// missing the local identity.
func NewZWingDialer(base EndpointDialer, cfg ZWingDialerConfig, logger log.Logger) (*ZWingDialer, error) {
	if cfg.LocalIdentity == nil {
		return nil, ErrZWingMissingIdentity
	}
	return &ZWingDialer{
		base: base,
		cfg: &zwing.Config{
			LocalIdentity:    cfg.LocalIdentity,
			HandshakeTimeout: cfg.HandshakeTimeout,
		},
		log: logger,
	}, nil
}

// DialEndpoint dials `endpoint` via the base dialer and upgrades the
// connection to a Z-Wing secure channel. Pass `expectedRemote` non-nil
// to pin the peer's expected public identity.
func (z *ZWingDialer) DialEndpoint(
	ctx context.Context,
	endpoint endpoints.Endpoint,
	expectedRemote *zwing.IdentityPublic,
) (net.Conn, error) {
	raw, err := z.base.DialEndpoint(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("zwing dialer: base dial %s: %w", endpoint, err)
	}
	return z.upgrade(raw, expectedRemote)
}

// Dial wraps the legacy IP-only path.
func (z *ZWingDialer) Dial(
	ctx context.Context,
	ip netip.AddrPort,
	expectedRemote *zwing.IdentityPublic,
) (net.Conn, error) {
	raw, err := z.base.Dial(ctx, ip)
	if err != nil {
		return nil, fmt.Errorf("zwing dialer: base dial %s: %w", ip, err)
	}
	return z.upgrade(raw, expectedRemote)
}

// Wrap upgrades an already-established `net.Conn` (e.g. an RNS link or
// a Unix socket) to a Z-Wing secure channel as the initiator.
func (z *ZWingDialer) Wrap(raw net.Conn, expectedRemote *zwing.IdentityPublic) (net.Conn, error) {
	return z.upgrade(raw, expectedRemote)
}

func (z *ZWingDialer) upgrade(raw net.Conn, expectedRemote *zwing.IdentityPublic) (net.Conn, error) {
	cfg := *z.cfg // shallow copy so we can plug in a per-dial pin without mutating the parent
	cfg.ExpectedRemote = expectedRemote
	conn, err := zwing.Client(raw, &cfg)
	if err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("zwing dialer: handshake: %w", err)
	}
	if z.log != nil {
		z.log.Debug("zwing channel up",
			log.Stringer("remote", raw.RemoteAddr()),
		)
	}
	return conn, nil
}

// ZWingListener wraps a raw TCP/RNS listener so every accepted
// connection is post-handshake by the time the caller sees it. The
// listener spec mirrors `zwing.Listen` but is built on top of any
// `net.Listener` to reuse the existing RNS / TCP listener plumbing.
type ZWingListener struct {
	raw net.Listener
	cfg *zwing.Config
	log log.Logger
}

// NewZWingListener wraps a raw listener with a Z-Wing handshake.
func NewZWingListener(raw net.Listener, cfg ZWingDialerConfig, logger log.Logger) (*ZWingListener, error) {
	if cfg.LocalIdentity == nil {
		return nil, ErrZWingMissingIdentity
	}
	return &ZWingListener{
		raw: raw,
		cfg: &zwing.Config{
			LocalIdentity:    cfg.LocalIdentity,
			HandshakeTimeout: cfg.HandshakeTimeout,
		},
		log: logger,
	}, nil
}

// Accept blocks until the next conn is handshaken. Failed handshakes
// surface as errors; callers that want to keep listening should call
// Accept again.
func (l *ZWingListener) Accept() (net.Conn, error) {
	raw, err := l.raw.Accept()
	if err != nil {
		return nil, err
	}
	conn, err := zwing.Server(raw, l.cfg)
	if err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("zwing listener: handshake: %w", err)
	}
	if l.log != nil {
		l.log.Debug("zwing channel accepted",
			log.Stringer("remote", raw.RemoteAddr()),
		)
	}
	return conn, nil
}

// Close closes the underlying listener.
func (l *ZWingListener) Close() error { return l.raw.Close() }

// Addr returns the underlying listener's address.
func (l *ZWingListener) Addr() net.Addr { return l.raw.Addr() }
