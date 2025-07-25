// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/consensus/sampling"
	"github.com/luxfi/node/utils/set"
)

var validParameters = sampling.Parameters{
	K:                     1,
	AlphaPreference:       1,
	AlphaConfidence:       1,
	Beta:                  1,
	ConcurrentRepolls:     1,
	OptimalProcessing:     1,
	MaxOutstandingItems:   1,
	MaxItemProcessingTime: 1,
}

func TestValid(t *testing.T) {
	tests := []struct {
		name        string
		s           Config
		expectedErr error
	}{
		{
			name: "invalid consensus parameters",
			s: Config{
				ConsensusParameters: sampling.Parameters{
					K:               2,
					AlphaPreference: 1,
				},
			},
			expectedErr: sampling.ErrParametersInvalid,
		},
		{
			name: "invalid allowed node IDs",
			s: Config{
				AllowedNodes:        set.Of(ids.GenerateTestNodeID()),
				ValidatorOnly:       false,
				ConsensusParameters: validParameters,
			},
			expectedErr: errAllowedNodesWhenNotValidatorOnly,
		},
		{
			name: "valid",
			s: Config{
				ConsensusParameters: validParameters,
				ValidatorOnly:       false,
			},
			expectedErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.s.Valid()
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}
