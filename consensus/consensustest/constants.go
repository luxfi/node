// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package consensustest

import (
	"github.com/luxfi/node/consensus/choices"
)

// Status constants for test assertions
var (
	// Accepted represents an accepted status
	Accepted = choices.Accepted
	
	// Rejected represents a rejected status
	Rejected = choices.Rejected
	
	// Processing represents a processing status
	Processing = choices.Processing
	
	// Unknown represents an unknown status
	Unknown = choices.Unknown
)