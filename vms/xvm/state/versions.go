<<<<<<< HEAD:vms/avm/state/versions.go
// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
=======
// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/state/versions.go
// See the file LICENSE for licensing terms.

package state

import "github.com/luxfi/ids"

type Versions interface {
	// GetState returns the state of the chain after [blkID] has been accepted.
	// If the state is not known, `false` will be returned.
	GetState(blkID ids.ID) (Chain, bool)
}
