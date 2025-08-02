// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package choices

import (
	"fmt"
)

// Status represents the current status of a block or vertex
type Status uint32

const (
	// Unknown indicates the status is not known
	Unknown Status = iota
	// Processing indicates the element is being processed
	Processing
	// Accepted indicates the element was accepted
	Accepted
	// Rejected indicates the element was rejected
	Rejected
	// Dropped indicates the element was dropped
	Dropped
	// Quantum indicates the element is in quantum consensus state
	Quantum
)

// Valid returns true if the status is a valid status.
func (s Status) Valid() bool {
	switch s {
	case Unknown, Processing, Accepted, Rejected, Dropped, Quantum:
		return true
	default:
		return false
	}
}

// Fetched returns true if the status implies the block has been fetched.
func (s Status) Fetched() bool {
	switch s {
	case Processing, Accepted, Rejected, Quantum:
		return true
	default:
		return false
	}
}

// Decided returns true if the status implies a decision has been made.
func (s Status) Decided() bool {
	switch s {
	case Accepted, Rejected, Quantum:
		return true
	default:
		return false
	}
}

// IsQuantum returns true if the status is Quantum.
func (s Status) IsQuantum() bool {
	return s == Quantum
}

// String returns a human-readable string for this status.
func (s Status) String() string {
	switch s {
	case Unknown:
		return "Unknown"
	case Processing:
		return "Processing"
	case Accepted:
		return "Accepted"
	case Rejected:
		return "Rejected"
	case Dropped:
		return "Dropped"
	case Quantum:
		return "Quantum"
	default:
		return fmt.Sprintf("Status(%d)", s)
	}
}

// MarshalJSON marshals the status as a string.
func (s Status) MarshalJSON() ([]byte, error) {
	return []byte("\"" + s.String() + "\""), nil
}

// Decidable represents an element that can be decided.
type Decidable interface {
	// ID returns the unique ID of this element.
	ID() string

	// Accept this element.
	Accept() error

	// Reject this element.
	Reject() error

	// Status returns the current status of this element.
	Status() Status
}

// TestDecidable is a test implementation of Decidable.
type TestDecidable struct {
	IDV        string
	AcceptV    error
	RejectV    error
	StatusV    Status
}

func (d *TestDecidable) ID() string     { return d.IDV }
func (d *TestDecidable) Accept() error  { return d.AcceptV }
func (d *TestDecidable) Reject() error  { return d.RejectV }
func (d *TestDecidable) Status() Status { return d.StatusV }