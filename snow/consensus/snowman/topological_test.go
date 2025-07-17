// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"testing"

	"github.com/luxfi/node/snow/consensus/snowball"
)

func TestTopological(t *testing.T) {
	runConsensusTests(t, TopologicalFactory{factory: snowball.SnowflakeFactory})
}
