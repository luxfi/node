// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"github.com/luxfi/node/v2/quasar/params"
)

// Factory returns new instances of Consensus
type Factory interface {
	New() Consensus
	Default() Parameters
}

// TopologicalFactory implements Factory by returning a topological consensus
type TopologicalFactory struct {
	Parameters params.Parameters
}

func (f TopologicalFactory) New() Consensus {
	return &Topological{}
}

func (f TopologicalFactory) Default() Parameters {
	return Parameters{
		K:                     f.Parameters.K,
		AlphaPreference:       f.Parameters.AlphaPreference,
		AlphaConfidence:       f.Parameters.AlphaConfidence,
		Beta:                  f.Parameters.Beta,
		ConcurrentRepolls:     f.Parameters.ConcurrentRepolls,
		OptimalProcessing:     f.Parameters.OptimalProcessing,
		MaxOutstandingItems:   f.Parameters.MaxOutstandingItems,
		MaxItemProcessingTime: int64(f.Parameters.MaxItemProcessingTime),
	}
}