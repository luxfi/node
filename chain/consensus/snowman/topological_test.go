// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"testing"

	"github.com/luxfi/node/chain/consensus/snowball"
)

func TestTopological(t *testing.T) {
	runConsensusTests(t, TopologicalFactory{factory: snowball.SnowflakeFactory})
}
