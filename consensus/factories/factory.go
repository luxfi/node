// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package factories

import (
	"github.com/luxfi/node/consensus/confidence"
	"github.com/luxfi/node/consensus/sampling"
	"github.com/luxfi/node/consensus/threshold"
	"github.com/luxfi/node/ids"
)

var (
	SnowballFactory  sampling.Factory = snowballFactory{}
	SnowflakeFactory sampling.Factory = snowflakeFactory{}
)

type snowballFactory struct{}

func (snowballFactory) NewNnary(params sampling.Parameters, choice ids.ID) sampling.Nnary {
	return confidence.NewNnarySnowball(params.AlphaPreference, params.AlphaConfidence, params.Beta, choice)
}

func (snowballFactory) NewUnary(params sampling.Parameters) sampling.Unary {
	return confidence.NewUnarySnowball(params.AlphaPreference, params.AlphaConfidence, params.Beta)
}

type snowflakeFactory struct{}

func (snowflakeFactory) NewNnary(params sampling.Parameters, choice ids.ID) sampling.Nnary {
	return threshold.NewNnaryThreshold(params.AlphaPreference, params.AlphaConfidence, params.Beta, choice)
}

func (snowflakeFactory) NewUnary(params sampling.Parameters) sampling.Unary {
	return threshold.NewUnaryThreshold(params.AlphaPreference, params.AlphaConfidence, params.Beta)
}