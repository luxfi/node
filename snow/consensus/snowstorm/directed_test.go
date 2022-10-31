// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"testing"
)

func TestDirectedConsensus(t *testing.T) { runConsensusTests(t, DirectedFactory{}, "DG") }
