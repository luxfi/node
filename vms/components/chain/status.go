// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

// Status represents the status of a block
type Status uint32

const (
	Unknown Status = iota
	Processing
	Rejected
	Accepted
)