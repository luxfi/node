// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linear

import (
	"testing"

	"github.com/luxfi/node/consensus/sampling"
)

func TestTopological(t *testing.T) {
	runConsensusTests(t, TopologicalFactory{factory: sampling.Factory})
}
