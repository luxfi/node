// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mpcvm

import (
	"encoding/json"
	"time"
)

// Config contains the configuration for the MVM
type Config struct {
	// General
	MempoolSize        int           `json:"mempoolSize"`
	BlockBuildInterval time.Duration `json:"blockBuildInterval"`

	// MPC
	MPCEnabled bool      `json:"mpcEnabled"`
	MPCConfig  MPCConfig `json:"mpcConfig"`

	// ZK
	ZKEnabled bool `json:"zkEnabled"`

	// Teleport
	TeleportEnabled bool           `json:"teleportEnabled"`
	IntentPoolSize  int            `json:"intentPoolSize"`
	ExecutorConfig  ExecutorConfig `json:"executorConfig"`

	// Warp
	WarpConfig interface{} `json:"warpConfig"`

	// P2P
	P2PConfig interface{} `json:"p2pConfig"`
}

// MPCConfig contains MPC-specific configuration
type MPCConfig struct {
	Threshold             int           `json:"threshold"`
	SessionTimeout        time.Duration `json:"sessionTimeout"`
	KeyGenTimeout         time.Duration `json:"keyGenTimeout"`
	SignTimeout           time.Duration `json:"signTimeout"`
	MaxSessions           int           `json:"maxSessions"`
	MaxConcurrentSessions int           `json:"maxConcurrentSessions"`
}

// ExecutorConfig contains executor-specific configuration
type ExecutorConfig struct {
	MaxConcurrentExecutions int           `json:"maxConcurrentExecutions"`
	ExecutionTimeout        time.Duration `json:"executionTimeout"`
}

// Parse parses the configuration from bytes
func (c *Config) Parse(configBytes []byte) error {
	if len(configBytes) == 0 {
		// Use default configuration
		c.MempoolSize = 1024
		c.BlockBuildInterval = 2 * time.Second
		c.MPCEnabled = true
		c.MPCConfig = MPCConfig{
			Threshold:             2,
			SessionTimeout:        5 * time.Minute,
			KeyGenTimeout:         2 * time.Minute,
			SignTimeout:           30 * time.Second,
			MaxSessions:           100,
			MaxConcurrentSessions: 50,
		}
		c.ZKEnabled = true
		c.TeleportEnabled = true
		c.IntentPoolSize = 10000
		c.ExecutorConfig = ExecutorConfig{
			MaxConcurrentExecutions: 100,
			ExecutionTimeout:        30 * time.Second,
		}
		return nil
	}
	return json.Unmarshal(configBytes, c)
}