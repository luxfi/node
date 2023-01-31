// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
)

// StateSummary represents all the information needed to download, verify, and
// rebuild its state.
type StateSummary interface {
	// ID uniquely identifies this state summary, regardless of the chain state.
	ID() ids.ID

	// Height uniquely identifies this an accepted state summary.
	Height() uint64

	// Bytes returns a byte slice than can be used to reconstruct this summary.
	Bytes() []byte

	// Accept triggers the VM to start state syncing to this summary.
	//
<<<<<<< HEAD
	// It returns the state sync mode selected by the VM.
	Accept(context.Context) (StateSyncMode, error)
=======
	// The returned boolean will be [true] if the VM has started state sync or
	// [false] if the VM has skipped state sync.
	Accept(context.Context) (bool, error)
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
}
