// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package factories

import (
	"github.com/luxfi/node/consensus/confidence"
	"github.com/luxfi/node/consensus/sampling"
	"github.com/luxfi/node/consensus/threshold"
	"github.com/luxfi/ids"
)

var (
	ConfidenceFactory     sampling.Factory = confidenceFactory{}
	ConsensusflakeFactory sampling.Factory = consensusflakeFactory{}
)

type confidenceFactory struct{}

func (confidenceFactory) NewNnary(params sampling.Parameters, choice ids.ID) sampling.Nnary {
	return confidence.NewNnaryConfidence(params.AlphaPreference, params.AlphaConfidence, params.Beta, choice)
}

func (confidenceFactory) NewUnary(params sampling.Parameters) sampling.Unary {
	return confidence.NewUnaryConfidence(params.AlphaPreference, params.AlphaConfidence, params.Beta)
}

type consensusflakeFactory struct{}

func (consensusflakeFactory) NewNnary(params sampling.Parameters, choice ids.ID) sampling.Nnary {
	return threshold.NewNnaryThreshold(params.AlphaPreference, params.AlphaConfidence, params.Beta, choice)
}

func (consensusflakeFactory) NewUnary(params sampling.Parameters) sampling.Unary {
	return threshold.NewUnaryThreshold(params.AlphaPreference, params.AlphaConfidence, params.Beta)
}