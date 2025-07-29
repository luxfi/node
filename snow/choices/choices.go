// Copyright (C) 2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package choices

import "fmt"

// Status represents the status of a decision
type Status uint32

const (
	// Unknown status
	Unknown Status = iota
	// Processing indicates the decision is being processed
	Processing
	// Rejected indicates the decision was rejected
	Rejected
	// Accepted indicates the decision was accepted
	Accepted
)

// String returns the string representation of the status
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
		return fmt.Sprintf("Status(%d)", s)
	}
}

// Valid returns true if the status is valid
func (s Status) Valid() bool {
	return s >= Unknown && s <= Accepted
}

// Decided returns true if the status is either Accepted or Rejected
func (s Status) Decided() bool {
	return s == Accepted || s == Rejected
}

// Fetched returns true if the status is at least Processing
func (s Status) Fetched() bool {
	return s >= Processing
}

// IsAccepted returns true if the status is Accepted
func (s Status) IsAccepted() bool {
	return s == Accepted
}

// IsRejected returns true if the status is Rejected
func (s Status) IsRejected() bool {
	return s == Rejected
}

// IsProcessing returns true if the status is Processing
func (s Status) IsProcessing() bool {
	return s == Processing
}

// IsUnknown returns true if the status is Unknown
func (s Status) IsUnknown() bool {
	return s == Unknown
}

// Decidable represents an element that can be decided
type Decidable interface {
	// ID returns the unique ID of this element
	ID() []byte

	// Accept accepts this element and changes its status to Accepted
	Accept() error

	// Reject rejects this element and changes its status to Rejected
	Reject() error

	// Status returns the current status
	Status() Status
}

// TestDecidable is a test implementation of Decidable
type TestDecidable struct {
	IDVal     []byte
	StatusVal Status
}

// ID implements Decidable
func (d *TestDecidable) ID() []byte {
	return d.IDVal
}

// Accept implements Decidable
func (d *TestDecidable) Accept() error {
	d.StatusVal = Accepted
	return nil
}

// Reject implements Decidable
func (d *TestDecidable) Reject() error {
	d.StatusVal = Rejected
	return nil
}

// Status implements Decidable
func (d *TestDecidable) Status() Status {
	return d.StatusVal
}
