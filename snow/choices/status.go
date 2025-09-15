// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package choices

// Status represents the status of a block or vertex
type Status uint8

const (
	// Unknown means the status is not known
	Unknown Status = iota
	// Processing means the block/vertex is being processed
	Processing
	// Rejected means the block/vertex was rejected
	Rejected
	// Accepted means the block/vertex was accepted
	Accepted
)

// Decided returns true if the status is either Accepted or Rejected
func (s Status) Decided() bool {
	return s == Accepted || s == Rejected
}

// String returns a string representation of the status
func (s Status) String() string {
	switch s {
	case Unknown:
		return "Unknown"
	case Processing:
		return "Processing"
	case Rejected:
		return "Rejected"
	case Accepted:
		return "Accepted"
	default:
		return "Invalid"
	}
}