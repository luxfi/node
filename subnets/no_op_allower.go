// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import "github.com/luxfi/ids"

// NoOpAllower is an Allower that always returns true
var NoOpAllower Allower = noOpAllower{}

type noOpAllower struct{}

func (noOpAllower) IsAllowed(ids.NodeID, bool) bool {
	return true
}
