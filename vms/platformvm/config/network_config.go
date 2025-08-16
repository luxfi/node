package config

import "time"

// NetworkConfig defines network configuration
type NetworkConfig struct {
	MaxGossipSize          int
	GossipAcceptedFrontier time.Duration
	GossipAccepted         time.Duration
}

// DefaultNetworkConfig returns default network configuration
var DefaultNetworkConfig = NetworkConfig{
	MaxGossipSize:          20,
	GossipAcceptedFrontier: 10 * time.Second,
	GossipAccepted:         10 * time.Second,
}
