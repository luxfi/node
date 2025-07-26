// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"strconv"

	_ "embed"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/ips"
	"github.com/luxfi/node/utils/sampler"
)

var (
	//go:embed bootstrappers.json
	bootstrappersPerNetworkJSON []byte

	bootstrappersPerNetwork map[string][]Bootstrapper
)

func init() {
	if err := json.Unmarshal(bootstrappersPerNetworkJSON, &bootstrappersPerNetwork); err != nil {
		panic(fmt.Sprintf("failed to decode bootstrappers.json %v", err))
	}
}

// BootstrapperRaw represents the raw JSON format with hostname support
type BootstrapperRaw struct {
	ID   ids.NodeID `json:"id"`
	Host string     `json:"host"` // Can be hostname:port or ip:port
}

// Represents the relationship between the nodeID and the nodeIP.
// The bootstrapper is sometimes called "anchor" or "beacon" node.
type Bootstrapper struct {
	ID   ids.NodeID     `json:"-"`
	Host string         `json:"-"` // Original host string for reference
	IP   netip.AddrPort `json:"-"` // Resolved IP address
}

// UnmarshalJSON implements custom JSON unmarshaling to support hostnames
func (b *Bootstrapper) UnmarshalJSON(data []byte) error {
	var raw BootstrapperRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	b.ID = raw.ID
	b.Host = raw.Host

	// Try to parse as IP:port first
	if addr, err := netip.ParseAddrPort(raw.Host); err == nil {
		b.IP = addr
		return nil
	}

	// If not a raw IP, try to resolve hostname
	host, portStr, err := net.SplitHostPort(raw.Host)
	if err != nil {
		return fmt.Errorf("invalid host format %q: %w", raw.Host, err)
	}

	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return fmt.Errorf("invalid port in %q: %w", raw.Host, err)
	}

	// Resolve hostname to IP
	ipAddr, err := ips.Lookup(host)
	if err != nil {
		// If we can't resolve now, store as-is and try again at runtime
		// For now, use an invalid address as placeholder
		b.IP = netip.AddrPort{}
		return nil
	}

	b.IP = netip.AddrPortFrom(ipAddr, uint16(port))
	return nil
}

// ResolveIP attempts to resolve the hostname to an IP address if not already resolved
func (b *Bootstrapper) ResolveIP() error {
	if b.IP.IsValid() {
		return nil // Already resolved
	}

	host, portStr, err := net.SplitHostPort(b.Host)
	if err != nil {
		return fmt.Errorf("invalid host format %q: %w", b.Host, err)
	}

	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return fmt.Errorf("invalid port in %q: %w", b.Host, err)
	}

	ipAddr, err := ips.Lookup(host)
	if err != nil {
		return fmt.Errorf("failed to resolve hostname %q: %w", host, err)
	}

	b.IP = netip.AddrPortFrom(ipAddr, uint16(port))
	return nil
}

// GetBootstrappers returns all default bootstrappers for the provided network.
func GetBootstrappers(networkID uint32) []Bootstrapper {
	networkName := constants.NetworkIDToNetworkName[networkID]
	return bootstrappersPerNetwork[networkName]
}

// SampleBootstrappers returns the some beacons this node should connect to
func SampleBootstrappers(networkID uint32, count int) []Bootstrapper {
	bootstrappers := GetBootstrappers(networkID)
	count = min(count, len(bootstrappers))

	s := sampler.NewUniform()
	s.Initialize(uint64(len(bootstrappers)))
	indices, _ := s.Sample(count)

	sampled := make([]Bootstrapper, 0, len(indices))
	for _, index := range indices {
		sampled = append(sampled, bootstrappers[int(index)])
	}
	return sampled
}
