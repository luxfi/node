// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dialer

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/net/ips"
	"github.com/luxfi/node/network/throttling"
)

var _ EndpointDialer = (*endpointDialer)(nil)

// EndpointDialer attempts to create a connection with either an IP:port or hostname:port.
type EndpointDialer interface {
	// DialEndpoint dials an endpoint that can be either IP or hostname based.
	DialEndpoint(ctx context.Context, endpoint ips.Endpoint) (net.Conn, error)

	// Dial is the legacy IP-only dial method for backward compatibility.
	Dial(ctx context.Context, ip netip.AddrPort) (net.Conn, error)
}

// DNSCacheConfig configures the DNS resolution cache.
type DNSCacheConfig struct {
	// TTL is how long resolved IPs are cached.
	TTL time.Duration `json:"ttl"`
	// MaxEntries is the maximum number of cached resolutions.
	MaxEntries int `json:"maxEntries"`
	// ResolveTimeout is the timeout for DNS resolution.
	ResolveTimeout time.Duration `json:"resolveTimeout"`
}

// DefaultDNSCacheConfig returns sensible defaults for DNS caching.
func DefaultDNSCacheConfig() DNSCacheConfig {
	return DNSCacheConfig{
		TTL:            5 * time.Minute,
		MaxEntries:     1000,
		ResolveTimeout: 5 * time.Second,
	}
}

type dnsEntry struct {
	addrs     []netip.Addr
	expiresAt time.Time
}

type endpointDialer struct {
	dialer    net.Dialer
	log       log.Logger
	network   string
	throttler throttling.DialThrottler
	dnsConfig DNSCacheConfig

	// DNS resolution cache
	dnsMu    sync.RWMutex
	dnsCache map[string]*dnsEntry
}

// EndpointDialerConfig extends Config with DNS settings.
type EndpointDialerConfig struct {
	Config
	DNSConfig DNSCacheConfig `json:"dnsConfig"`
}

// NewEndpointDialer creates a dialer that supports both IP and hostname endpoints.
func NewEndpointDialer(network string, config EndpointDialerConfig, logger log.Logger) EndpointDialer {
	var throttler throttling.DialThrottler
	if config.ThrottleRps <= 0 {
		throttler = throttling.NewNoDialThrottler()
	} else {
		throttler = throttling.NewDialThrottler(int(config.ThrottleRps))
	}

	dnsConfig := config.DNSConfig
	if dnsConfig.TTL == 0 {
		dnsConfig = DefaultDNSCacheConfig()
	}

	logger.Debug(
		"creating endpoint dialer",
		log.Uint32("throttleRPS", config.ThrottleRps),
		log.Duration("dialTimeout", config.ConnectionTimeout),
		log.Duration("dnsTTL", dnsConfig.TTL),
		log.Int("dnsMaxEntries", dnsConfig.MaxEntries),
	)

	return &endpointDialer{
		dialer:    net.Dialer{Timeout: config.ConnectionTimeout},
		log:       logger,
		network:   network,
		throttler: throttler,
		dnsConfig: dnsConfig,
		dnsCache:  make(map[string]*dnsEntry),
	}
}

// DialEndpoint dials an endpoint, handling both IP and hostname targets.
func (d *endpointDialer) DialEndpoint(ctx context.Context, endpoint ips.Endpoint) (net.Conn, error) {
	if err := d.throttler.Acquire(ctx); err != nil {
		return nil, err
	}

	var target string
	if endpoint.IsIP() {
		target = endpoint.AddrPort.String()
	} else {
		// Hostname endpoint - try to use cached resolution first
		addr, err := d.resolveHostname(ctx, endpoint.Hostname)
		if err != nil {
			// Fall back to letting net.Dial resolve the hostname
			d.log.Debug("DNS cache miss, using system resolver",
				log.String("hostname", endpoint.Hostname),
				log.Err(err),
			)
			target = endpoint.String()
		} else {
			// Use cached IP
			target = netip.AddrPortFrom(addr, endpoint.Port).String()
		}
	}

	d.log.Debug("dialing endpoint",
		log.String("original", endpoint.String()),
		log.String("target", target),
	)

	conn, err := d.dialer.DialContext(ctx, d.network, target)
	if err != nil {
		return nil, fmt.Errorf("error dialing %s: %w", endpoint, err)
	}
	return conn, nil
}

// Dial implements the legacy IP-only Dialer interface.
func (d *endpointDialer) Dial(ctx context.Context, ip netip.AddrPort) (net.Conn, error) {
	return d.DialEndpoint(ctx, ips.NewIPEndpoint(ip))
}

// resolveHostname resolves a hostname to an IP, using cache when possible.
func (d *endpointDialer) resolveHostname(ctx context.Context, hostname string) (netip.Addr, error) {
	// Check cache first
	d.dnsMu.RLock()
	entry, ok := d.dnsCache[hostname]
	d.dnsMu.RUnlock()

	if ok && time.Now().Before(entry.expiresAt) && len(entry.addrs) > 0 {
		// Return first cached address (could round-robin for load balancing)
		return entry.addrs[0], nil
	}

	// Resolve with timeout
	resolveCtx, cancel := context.WithTimeout(ctx, d.dnsConfig.ResolveTimeout)
	defer cancel()

	resolver := net.DefaultResolver
	ips, err := resolver.LookupNetIP(resolveCtx, "ip", hostname)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("failed to resolve %s: %w", hostname, err)
	}
	if len(ips) == 0 {
		return netip.Addr{}, fmt.Errorf("no addresses found for %s", hostname)
	}

	// Update cache
	d.dnsMu.Lock()
	// Enforce max entries
	if len(d.dnsCache) >= d.dnsConfig.MaxEntries {
		d.evictOldestDNSEntry()
	}
	d.dnsCache[hostname] = &dnsEntry{
		addrs:     ips,
		expiresAt: time.Now().Add(d.dnsConfig.TTL),
	}
	d.dnsMu.Unlock()

	d.log.Debug("resolved hostname",
		log.String("hostname", hostname),
		log.Stringer("addr", ips[0]),
		log.Duration("ttl", d.dnsConfig.TTL),
	)

	return ips[0], nil
}

// evictOldestDNSEntry removes the oldest DNS cache entry.
// Caller must hold dnsMu write lock.
func (d *endpointDialer) evictOldestDNSEntry() {
	var oldestHost string
	oldestTime := time.Now().Add(24 * time.Hour) // Far future

	for host, entry := range d.dnsCache {
		if entry.expiresAt.Before(oldestTime) {
			oldestTime = entry.expiresAt
			oldestHost = host
		}
	}

	if oldestHost != "" {
		delete(d.dnsCache, oldestHost)
	}
}

// InvalidateDNS removes a hostname from the DNS cache, forcing re-resolution.
func (d *endpointDialer) InvalidateDNS(hostname string) {
	d.dnsMu.Lock()
	delete(d.dnsCache, hostname)
	d.dnsMu.Unlock()
}

// ClearDNSCache clears the entire DNS cache.
func (d *endpointDialer) ClearDNSCache() {
	d.dnsMu.Lock()
	d.dnsCache = make(map[string]*dnsEntry)
	d.dnsMu.Unlock()
}
