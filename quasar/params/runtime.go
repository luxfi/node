// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

)

var (
	// RuntimeParams holds the current runtime consensus parameters
	RuntimeParams Parameters
	
	// runtimeMu protects RuntimeParams during updates
	runtimeMu sync.RWMutex
	
	// initialized tracks if runtime params have been set
	initialized bool
)

// Initialize sets the runtime parameters based on network name
func Initialize(network string) {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	
	RuntimeParams = GetParams(network)
	initialized = true
}

// Get returns the current runtime consensus parameters
func Get() Parameters {
	runtimeMu.RLock()
	defer runtimeMu.RUnlock()
	
	if !initialized {
		// Default to testnet if not initialized
		return TestnetParams
	}
	
	return RuntimeParams
}

// Override updates specific runtime parameters
// This allows consensus tools to modify parameters at runtime
func Override(updates map[string]interface{}) error {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	
	if !initialized {
		RuntimeParams = TestnetParams
		initialized = true
	}
	
	// Create a copy to modify
	params := RuntimeParams
	
	// Apply updates
	for key, value := range updates {
		switch key {
		case "K", "k":
			if v, ok := value.(int); ok {
				params.K = v
			}
		case "AlphaPreference", "alphaPreference":
			if v, ok := value.(int); ok {
				params.AlphaPreference = v
			}
		case "AlphaConfidence", "alphaConfidence":
			if v, ok := value.(int); ok {
				params.AlphaConfidence = v
			}
		case "Beta", "beta":
			if v, ok := value.(int); ok {
				params.Beta = v
			}
		case "ConcurrentRepolls", "concurrentRepolls":
			if v, ok := value.(int); ok {
				params.ConcurrentRepolls = v
			}
		case "OptimalProcessing", "optimalProcessing":
			if v, ok := value.(int); ok {
				params.OptimalProcessing = v
			}
		case "MaxOutstandingItems", "maxOutstandingItems":
			if v, ok := value.(int); ok {
				params.MaxOutstandingItems = v
			}
		case "MaxItemProcessingTime", "maxItemProcessingTime":
			if v, ok := value.(string); ok {
				duration, err := time.ParseDuration(v)
				if err != nil {
					return fmt.Errorf("invalid duration for maxItemProcessingTime: %w", err)
				}
				params.MaxItemProcessingTime = duration
			}
		case "MinRoundInterval", "minRoundInterval":
			if v, ok := value.(string); ok {
				duration, err := time.ParseDuration(v)
				if err != nil {
					return fmt.Errorf("invalid duration for minRoundInterval: %w", err)
				}
				params.MinRoundInterval = duration
			}
		default:
			return fmt.Errorf("unknown parameter: %s", key)
		}
	}
	
	// Validate the new parameters
	if err := params.Valid(); err != nil {
		return fmt.Errorf("invalid parameters: %w", err)
	}
	
	// Update runtime params
	RuntimeParams = params
	return nil
}

// LoadFromFile loads consensus parameters from a JSON file
func LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	
	var params Parameters
	if err := json.Unmarshal(data, &params); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}
	
	if err := params.Valid(); err != nil {
		return fmt.Errorf("invalid parameters in config file: %w", err)
	}
	
	runtimeMu.Lock()
	RuntimeParams = params
	initialized = true
	runtimeMu.Unlock()
	
	return nil
}

// SaveToFile saves the current runtime parameters to a JSON file
func SaveToFile(path string) error {
	runtimeMu.RLock()
	params := RuntimeParams
	runtimeMu.RUnlock()
	
	data, err := json.MarshalIndent(params, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal parameters: %w", err)
	}
	
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	
	return nil
}

// Reset resets runtime parameters to defaults for the given network
func Reset(network string) {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	
	RuntimeParams = GetParams(network)
	initialized = true
}