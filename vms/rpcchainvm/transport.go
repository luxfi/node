// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import "time"

// Transport specifies the RPC transport protocol for VM communication
type Transport string

const (
	// TransportGRPC uses gRPC with protobuf serialization (default)
	TransportGRPC Transport = "grpc"
	// TransportZAP uses ZAP zero-copy serialization over TCP
	TransportZAP Transport = "zap"
	// TransportBoth enables both gRPC and ZAP transports
	TransportBoth Transport = "both"
)

// TransportConfig contains transport configuration
type TransportConfig struct {
	// Transport specifies which transport(s) to use
	Transport Transport
	// Timeout for transport operations
	Timeout time.Duration
}

// DefaultTransportConfig returns the default transport configuration
// Uses ZAP by default for high-performance zero-copy communication.
// gRPC can be used with -tags=grpc build flag for compatibility testing.
func DefaultTransportConfig() TransportConfig {
	return TransportConfig{
		Transport: TransportZAP,
		Timeout:   30 * time.Second,
	}
}

// ValidTransports returns all valid transport values
func ValidTransports() []Transport {
	return []Transport{TransportGRPC, TransportZAP, TransportBoth}
}

// IsValid returns true if the transport is a valid value
func (t Transport) IsValid() bool {
	switch t {
	case TransportGRPC, TransportZAP, TransportBoth:
		return true
	default:
		return false
	}
}

// UsesGRPC returns true if this transport configuration uses gRPC
func (t Transport) UsesGRPC() bool {
	return t == TransportGRPC || t == TransportBoth
}

// UsesZAP returns true if this transport configuration uses ZAP
func (t Transport) UsesZAP() bool {
	return t == TransportZAP || t == TransportBoth
}
