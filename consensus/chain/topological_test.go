// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"testing"
	

	"github.com/luxfi/node/consensus/factories"
)

func TestTopological(t *testing.T) {
	runConsensusTests(t, TopologicalFactory{factory: factories.ConsensusflakeFactory})
}
