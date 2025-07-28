// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linear

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"gonum.org/v1/gonum/mathext/prng"

	"github.com/luxfi/node/consensus/factories"
	"github.com/luxfi/node/consensus/sampling"
)

func TestConvergenceSampling(t *testing.T) {
	require := require.New(t)

	params := sampling.Parameters{
		K:                     20,
		AlphaPreference:       11,
		AlphaConfidence:       11,
		Beta:                  20,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	for peerCount := 20; peerCount < 2000; peerCount *= 10 {
		numNodes := peerCount

		t.Run(fmt.Sprintf("%d nodes", numNodes), func(t *testing.T) {
			n := NewNetwork(params, 10, prng.NewMT19937())
			for i := 0; i < numNodes; i++ {
				var sbFactory sampling.Factory
				if i%2 == 0 {
					sbFactory = factories.ConsensusflakeFactory
				} else {
					sbFactory = factories.ConfidenceFactory
				}

				factory := TopologicalFactory{factory: sbFactory}
				sm := factory.New()
				require.NoError(n.AddNode(t, sm))
			}

			for !n.Finalized() {
				require.NoError(n.Round())
			}

			require.True(n.Agreement())
		})
	}
}
