// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package lux

import (
	"testing"
)

func TestTopological(t *testing.T) { runConsensusTests(t, TopologicalFactory{}) }