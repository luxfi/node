// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Job defines the interface required to be placed on the job queue.
type Job interface {
	ID() ids.ID
<<<<<<< HEAD
<<<<<<< HEAD
	MissingDependencies(context.Context) (set.Set[ids.ID], error)
=======
	MissingDependencies(context.Context) (ids.Set, error)
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
=======
	MissingDependencies(context.Context) (set.Set[ids.ID], error)
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
	// Returns true if this job has at least 1 missing dependency
	HasMissingDependencies(context.Context) (bool, error)
	Execute(context.Context) error
	Bytes() []byte
}
