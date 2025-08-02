// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"time"
)

// Example consensus parameter configurations for different use cases

// HighThroughputParams optimized for maximum throughput with lower fault tolerance
// Suitable for permissioned networks or high-trust environments
var HighThroughputParams = Parameters{
	K:                     7,
	AlphaPreference:       5,                       // tolerate up to 2 failures
	AlphaConfidence:       6,                       // tolerate up to 1 failure
	Beta:                  3,                       // fast finality
	ConcurrentRepolls:     64,                      // aggressive pipelining
	OptimalProcessing:     64,
	MaxOutstandingItems:   1024,
	MaxItemProcessingTime: 2 * time.Second,
	MinRoundInterval:      5 * time.Millisecond,    // very fast rounds
}

// HighSecurityParams optimized for maximum Byzantine fault tolerance
// Suitable for adversarial environments with potential attacks
var HighSecurityParams = Parameters{
	K:                     31,
	AlphaPreference:       21,                      // tolerate up to 10 failures
	AlphaConfidence:       26,                      // tolerate up to 5 failures
	Beta:                  15,                      // high confidence threshold
	ConcurrentRepolls:     4,                       // conservative pipelining
	OptimalProcessing:     5,
	MaxOutstandingItems:   128,
	MaxItemProcessingTime: 30 * time.Second,
	MinRoundInterval:      500 * time.Millisecond,  // slower, more deliberate
}

// MobileNetworkParams optimized for mobile/edge devices with variable connectivity
// Suitable for IoT or mobile validator networks
var MobileNetworkParams = Parameters{
	K:                     9,
	AlphaPreference:       6,                       // tolerate up to 3 failures
	AlphaConfidence:       7,                       // tolerate up to 2 failures
	Beta:                  12,                      // higher beta for network instability
	ConcurrentRepolls:     6,
	OptimalProcessing:     8,
	MaxOutstandingItems:   64,
	MaxItemProcessingTime: 20 * time.Second,       // longer timeout for poor connectivity
	MinRoundInterval:      300 * time.Millisecond,  // account for higher latency
}

// SubnetParams optimized for subnet/shard consensus with smaller validator sets
// Suitable for application-specific subnets
var SubnetParams = Parameters{
	K:                     7,
	AlphaPreference:       5,                       // tolerate up to 2 failures
	AlphaConfidence:       6,                       // tolerate up to 1 failure
	Beta:                  6,                       // balanced finality
	ConcurrentRepolls:     12,
	OptimalProcessing:     16,
	MaxOutstandingItems:   256,
	MaxItemProcessingTime: 5 * time.Second,
	MinRoundInterval:      50 * time.Millisecond,   // faster than mainnet
}

// GeographicallyDistributedParams for validators spread across continents
// Accounts for high latency between regions
var GeographicallyDistributedParams = Parameters{
	K:                     15,
	AlphaPreference:       10,                      // tolerate up to 5 failures
	AlphaConfidence:       12,                      // tolerate up to 3 failures
	Beta:                  10,                      // account for network delays
	ConcurrentRepolls:     8,
	OptimalProcessing:     10,
	MaxOutstandingItems:   512,
	MaxItemProcessingTime: 15 * time.Second,
	MinRoundInterval:      400 * time.Millisecond,  // account for intercontinental latency
}

// ExampleConfigs provides a map of example configurations
var ExampleConfigs = map[string]Parameters{
	"high-throughput":    HighThroughputParams,
	"high-security":      HighSecurityParams,
	"mobile":             MobileNetworkParams,
	"subnet":             SubnetParams,
	"geo-distributed":    GeographicallyDistributedParams,
}

// GetExample returns an example configuration by name
func GetExample(name string) (Parameters, bool) {
	params, ok := ExampleConfigs[name]
	return params, ok
}