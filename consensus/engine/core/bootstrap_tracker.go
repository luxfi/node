// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import "github.com/luxfi/node/ids"

// BootstrapTracker describes the standard interface for tracking the status of
// a subnet bootstrapping
type BootstrapTracker interface {
	// Returns true iff done bootstrapping
	IsBootstrapped() bool

	// Bootstrapped marks the named chain as being bootstrapped
	Bootstrapped(chainID ids.ID)

	// AllBootstrapped returns a channel that is closed when all chains in this
	// subnet have been bootstrapped
	AllBootstrapped() <-chan struct{}
}
