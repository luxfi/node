// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dialer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"sync"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/net/endpoints"
)

var (
	// ErrRNSLinkClosed is returned when operating on a closed RNS link.
	ErrRNSLinkClosed = errors.New("RNS link closed")
	// ErrRNSTimeout is returned when an RNS operation times out.
	ErrRNSTimeout = errors.New("RNS operation timed out")
)

// RNSConfig configures the Reticulum Network Stack transport.
type RNSConfig struct {
	// ConfigPath is the path to the Reticulum config directory.
	// If empty, uses default ~/.reticulum/
	ConfigPath string `json:"configPath"`

	// IdentityPath is where to store/load the RNS identity.
	// If empty, defaults to ConfigPath/identity
	IdentityPath string `json:"identityPath"`

	// GatewayAddr is an optional RNS gateway for initial connectivity.
	// Format: "host:port"
	GatewayAddr string `json:"gatewayAddr"`

	// AnnounceInterval is how often to re-announce our destination.
	AnnounceInterval time.Duration `json:"announceInterval"`

	// Interfaces configures which RNS interfaces to use.
	// Examples: "AutoInterface", "TCPClientInterface", "LoRaInterface"
	Interfaces []string `json:"interfaces"`

	// LinkTimeout is the timeout for establishing RNS links.
	LinkTimeout time.Duration `json:"linkTimeout"`

	// Enabled controls whether RNS transport is active.
	Enabled bool `json:"enabled"`
}

// DefaultRNSConfig returns sensible defaults for RNS transport.
func DefaultRNSConfig() RNSConfig {
	return RNSConfig{
		ConfigPath:       "", // Use default ~/.reticulum/
		IdentityPath:     "",
		GatewayAddr:      "",
		AnnounceInterval: 5 * time.Minute,
		Interfaces:       []string{"AutoInterface"},
		LinkTimeout:      30 * time.Second,
		Enabled:          false, // Disabled by default
	}
}

// rnsTransport implements RNSTransport using Reticulum Network Stack.
// This provides medium-agnostic connectivity over LoRa, mesh, serial, etc.
type rnsTransport struct {
	config    RNSConfig
	log       log.Logger
	available bool

	// Our identity
	identity *RNSIdentity

	// Announcer for destination discovery
	announcer *RNSAnnouncer

	// Connection management
	mu    sync.RWMutex
	links map[string]*rnsConn // destination hex -> active connection
}

// NewRNSTransport creates an RNS transport with the given configuration.
// The transport must be started with Start() before use.
func NewRNSTransport(config RNSConfig, logger log.Logger) RNSTransport {
	return &rnsTransport{
		config: config,
		log:    logger,
		links:  make(map[string]*rnsConn),
	}
}

// Start initializes the Reticulum transport.
// This should be called after node startup to enable mesh connectivity.
func (t *rnsTransport) Start() error {
	if !t.config.Enabled {
		t.log.Debug("RNS transport disabled")
		return nil
	}

	t.log.Info("starting RNS transport",
		log.String("configPath", t.config.ConfigPath),
		log.Strings("interfaces", t.config.Interfaces),
	)

	// Resolve identity path
	identityPath := t.config.IdentityPath
	if identityPath == "" {
		configPath := t.config.ConfigPath
		if configPath == "" {
			configPath = "~/.reticulum"
		}
		identityPath = filepath.Join(configPath, "identity")
	}

	// Load or generate identity
	var err error
	t.identity, err = LoadOrGenerateIdentity(identityPath)
	if err != nil {
		return fmt.Errorf("failed to load/generate identity: %w", err)
	}

	dest := t.identity.Destination()
	t.log.Info("RNS identity loaded",
		log.String("destination", destinationHex(dest)),
	)

	// Set up announcer for destination discovery
	announcerConfig := RNSAnnouncerConfig{
		AnnounceInterval: t.config.AnnounceInterval,
		GatewayAddr:      t.config.GatewayAddr,
	}
	t.announcer = NewRNSAnnouncer(t.identity, announcerConfig, t.log)

	// Start announcer (runs in background)
	t.announcer.Start()

	t.available = true
	t.log.Info("RNS transport started",
		log.String("destination", destinationHex(dest)),
	)
	return nil
}

// Dial establishes a link to an RNS destination.
func (t *rnsTransport) Dial(ctx context.Context, destination [endpoints.RNSDestinationLen]byte) (net.Conn, error) {
	if !t.available {
		return nil, ErrRNSNotConfigured
	}

	destHex := destinationHex(destination)

	t.log.Debug("dialing RNS destination",
		log.String("destination", destHex),
	)

	// Check for existing link
	t.mu.RLock()
	existing, ok := t.links[destHex]
	t.mu.RUnlock()
	if ok && !existing.isClosed() {
		return existing, nil
	}

	// Create new link with timeout
	linkCtx, cancel := context.WithTimeout(ctx, t.config.LinkTimeout)
	defer cancel()

	conn, err := t.dialLink(linkCtx, destination)
	if err != nil {
		return nil, err
	}

	// Store link
	t.mu.Lock()
	t.links[destHex] = conn
	t.mu.Unlock()

	return conn, nil
}

// dialLink creates a new RNS link to the destination.
func (t *rnsTransport) dialLink(ctx context.Context, destination [endpoints.RNSDestinationLen]byte) (*rnsConn, error) {
	// Look up destination in announce table
	announce := t.announcer.Lookup(destination)
	var transportAddr netip.AddrPort
	if announce == nil {
		// Destination unknown - try gateway if configured
		if t.config.GatewayAddr == "" {
			return nil, fmt.Errorf("%w: %s", ErrDestinationUnknown, destinationHex(destination))
		}
		// Use gateway for routing
		transportAddr = parseAddrPort(t.config.GatewayAddr)
	} else {
		// Use transport address from announcement if available
		entry := &AnnounceEntry{
			Destination: announce.Destination,
			SigningKey:  announce.Ed25519PubKey[:],
			ExchangeKey: announce.X25519PubKey,
			Hops:        announce.Hops,
		}
		if entry.TransportAddr.IsValid() {
			transportAddr = entry.TransportAddr
		}
	}

	// Establish TCP connection to transport address
	var tcpConn net.Conn
	if transportAddr.IsValid() {
		var dialer net.Dialer
		var dialErr error
		tcpConn, dialErr = dialer.DialContext(ctx, "tcp", transportAddr.String())
		if dialErr != nil {
			return nil, fmt.Errorf("failed to connect to transport: %w", dialErr)
		}
	} else if t.config.GatewayAddr != "" {
		// Fall back to gateway
		var dialer net.Dialer
		var dialErr error
		tcpConn, dialErr = dialer.DialContext(ctx, "tcp", t.config.GatewayAddr)
		if dialErr != nil {
			return nil, fmt.Errorf("failed to connect to gateway: %w", dialErr)
		}
	} else {
		return nil, fmt.Errorf("%w: no transport path to destination", ErrDestinationUnknown)
	}

	// Set up deadline for handshake
	if deadline, ok := ctx.Deadline(); ok {
		tcpConn.SetDeadline(deadline)
	}

	// Create RNS link over TCP connection
	link := NewRNSLink(tcpConn, t.identity)

	// Perform handshake
	if err := link.Handshake(true, destination); err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("handshake failed: %w", err)
	}

	// Clear deadline after handshake
	tcpConn.SetDeadline(time.Time{})

	// Wrap link in rnsConn
	conn := &rnsConn{
		destination: destination,
		link:        link,
	}

	t.log.Debug("RNS link established",
		log.String("destination", destinationHex(destination)),
	)

	return conn, nil
}

// Available returns true if RNS transport is ready.
func (t *rnsTransport) Available() bool {
	return t.available
}

// Close shuts down the RNS transport.
func (t *rnsTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Already closed
	if !t.available && t.identity == nil && t.announcer == nil {
		return nil
	}

	// Close all links
	for _, link := range t.links {
		link.Close()
	}
	t.links = make(map[string]*rnsConn)

	// Stop announcer
	if t.announcer != nil {
		t.announcer.Stop()
		t.announcer = nil
	}

	// Close identity
	if t.identity != nil {
		t.identity.Close()
		t.identity = nil
	}

	t.available = false
	t.log.Info("RNS transport closed")
	return nil
}

// Announce creates and returns our announcement (for external broadcast).
func (t *rnsTransport) Announce() (*Announce, error) {
	if t.announcer == nil {
		return nil, ErrRNSNotConfigured
	}
	return t.announcer.CreateAnnounce()
}

// RegisterAnnounceHandler adds a handler for incoming announcements.
func (t *rnsTransport) RegisterAnnounceHandler(handler AnnounceHandler) {
	if t.announcer != nil {
		t.announcer.AddHandler(handler)
	}
}

// GetDestinationTable returns known destinations.
func (t *rnsTransport) GetDestinationTable() *DestTable {
	if t.announcer == nil {
		return nil
	}
	return t.announcer.DestTable()
}

// Destination returns our local RNS destination.
func (t *rnsTransport) Destination() [endpoints.RNSDestinationLen]byte {
	if t.identity == nil {
		return [endpoints.RNSDestinationLen]byte{}
	}
	return t.identity.Destination()
}

// Identity returns the local RNS identity.
func (t *rnsTransport) Identity() *RNSIdentity {
	return t.identity
}

// rnsConn wraps an RNS Link as a net.Conn.
type rnsConn struct {
	destination [endpoints.RNSDestinationLen]byte
	link        *RNSLink

	mu     sync.Mutex
	closed bool
}

// Read reads data from the RNS link.
func (c *rnsConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, ErrRNSLinkClosed
	}
	link := c.link
	c.mu.Unlock()

	if link == nil {
		return 0, ErrRNSLinkClosed
	}

	return link.Read(b)
}

// Write writes data to the RNS link.
func (c *rnsConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, ErrRNSLinkClosed
	}
	link := c.link
	c.mu.Unlock()

	if link == nil {
		return 0, ErrRNSLinkClosed
	}

	return link.Write(b)
}

// Close closes the RNS link.
func (c *rnsConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if c.link != nil {
		return c.link.Close()
	}
	return nil
}

// isClosed returns true if the connection is closed.
func (c *rnsConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// LocalAddr returns the local RNS address.
func (c *rnsConn) LocalAddr() net.Addr {
	if c.link != nil {
		return c.link.LocalAddr()
	}
	return &rnsAddr{local: true}
}

// RemoteAddr returns the remote RNS destination as an address.
func (c *rnsConn) RemoteAddr() net.Addr {
	return &rnsAddr{destination: c.destination}
}

// SetDeadline sets both read and write deadlines.
func (c *rnsConn) SetDeadline(t time.Time) error {
	if c.link != nil {
		return c.link.SetDeadline(t)
	}
	return nil
}

// SetReadDeadline sets the read deadline.
func (c *rnsConn) SetReadDeadline(t time.Time) error {
	if c.link != nil {
		return c.link.SetReadDeadline(t)
	}
	return nil
}

// SetWriteDeadline sets the write deadline.
func (c *rnsConn) SetWriteDeadline(t time.Time) error {
	if c.link != nil {
		return c.link.SetWriteDeadline(t)
	}
	return nil
}

// rnsAddr implements net.Addr for RNS destinations.
type rnsAddr struct {
	destination [endpoints.RNSDestinationLen]byte
	local       bool
}

func (a *rnsAddr) Network() string {
	return "rns"
}

func (a *rnsAddr) String() string {
	if a.local {
		return "rns://local"
	}
	return "rns://" + destinationHex(a.destination)
}

// destinationHex returns the destination as a hex string.
func destinationHex(dest [endpoints.RNSDestinationLen]byte) string {
	const hexChars = "0123456789abcdef"
	buf := make([]byte, endpoints.RNSDestinationLen*2)
	for i, b := range dest {
		buf[i*2] = hexChars[b>>4]
		buf[i*2+1] = hexChars[b&0x0f]
	}
	return string(buf)
}

// parseAddrPort parses a host:port string to netip.AddrPort.
func parseAddrPort(addr string) (ap netip.AddrPort) {
	if addr == "" {
		return
	}
	// net.SplitHostPort handles IPv6 brackets
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return
	}
	// Try to parse as IP first
	ip := net.ParseIP(host)
	if ip == nil {
		// Might be a hostname, try to resolve
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			return
		}
		ip = ips[0]
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	if port <= 0 || port > 65535 {
		return
	}
	if ip4 := ip.To4(); ip4 != nil {
		return netip.AddrPortFrom(netip.AddrFrom4([4]byte(ip4)), uint16(port))
	}
	if ip6 := ip.To16(); ip6 != nil {
		return netip.AddrPortFrom(netip.AddrFrom16([16]byte(ip6)), uint16(port))
	}
	return
}

// Compile-time interface checks
var (
	_ RNSTransport = (*rnsTransport)(nil)
	_ net.Conn     = (*rnsConn)(nil)
	_ net.Addr     = (*rnsAddr)(nil)
)
