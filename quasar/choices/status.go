// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package choices

import (
	"fmt"
)

// Status represents the current status of a block or vertex
type Status uint32

const (
	// Unknown means the status is not known
	Unknown Status = iota
	// Processing means the block is being processed
	Processing
	// Rejected means the block was rejected
	Rejected
	// Accepted means the block was accepted with dual certificates
	Accepted
	// Quantum means the block has quantum-secure finality
	Quantum
)

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
	case Quantum:
		return "Quantum"
	default:
		return fmt.Sprintf("Status(%d)", s)
	}
}

// Valid returns true if the status is a valid status
func (s Status) Valid() bool {
	return s <= Quantum
}

// Fetched returns true if the block has been fetched
func (s Status) Fetched() bool {
	switch s {
	case Unknown:
		return false
	default:
		return true
	}
}

// Decided returns true if the block has been decided
func (s Status) Decided() bool {
	switch s {
	case Unknown, Processing:
		return false
	default:
		return true
	}
}

// IsAccepted returns true if the block was accepted
func (s Status) IsAccepted() bool {
	switch s {
	case Accepted, Quantum:
		return true
	default:
		return false
	}
}

// IsQuantum returns true if the block has quantum-secure finality
func (s Status) IsQuantum() bool {
	return s == Quantum
}

// Preference returns the preferred status
type Preference struct {
	// Status is the current status
	Status Status
	// DualCert indicates if both BLS and RT certificates are present
	DualCert bool
}

// TestDecidable is a test interface for decidable blocks
type TestDecidable struct {
	IDV         string
	StatusV     Status
	PreferenceV Preference
}

func (t *TestDecidable) ID() string      { return t.IDV }
func (t *TestDecidable) Status() Status   { return t.StatusV }
func (t *TestDecidable) Accept() error   { t.StatusV = Accepted; return nil }
func (t *TestDecidable) Reject() error   { t.StatusV = Rejected; return nil }
func (t *TestDecidable) SetQuantum() error { t.StatusV = Quantum; return nil }

// Decidable represents an element that can be decided
type Decidable interface {
	// ID returns the unique ID of this element
	ID() string
	// Accept marks this element as accepted
	Accept() error
	// Reject marks this element as rejected
	Reject() error
	// Status returns the current status
	Status() Status
}

// Quasar extends Decidable with quantum-secure finality
type Quasar interface {
	Decidable
	// SetQuantum marks this element as having quantum-secure finality
	SetQuantum() error
	// HasDualCert returns true if both BLS and RT certificates are present
	HasDualCert() bool
}