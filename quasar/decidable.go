// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar/choices"
)

// Decidable represents an element that can be decided by consensus
type Decidable interface {
	// ID returns the unique ID of this element
	ID() ids.ID

	// Accept marks this element as accepted
	Accept() error

	// Reject marks this element as rejected
	Reject() error

	// Status returns the current status
	Status() choices.Status
}

// Block represents a block that can be decided by consensus
type Block interface {
	Decidable

	// Parent returns the ID of this block's parent
	Parent() ids.ID

	// Verify that the state transition this block would make is valid
	Verify() error

	// Bytes returns the binary representation of this block
	Bytes() []byte

	// Height returns the height of this block
	Height() uint64

	// Timestamp returns the timestamp of this block
	Timestamp() int64
}

// Vertex represents a vertex in a DAG that can be decided by consensus
type Vertex interface {
	Decidable

	// Parents returns the IDs of this vertex's parents
	Parents() []ids.ID

	// Verify that the state transition this vertex would make is valid
	Verify() error

	// Bytes returns the binary representation of this vertex
	Bytes() []byte

	// Height returns the height of this vertex
	Height() uint64

	// Epoch returns the epoch of this vertex
	Epoch() uint32

	// Txs returns the transactions in this vertex
	Txs() [][]byte
}

// QuasarBlock extends Block with quantum-secure features
type QuasarBlock interface {
	Block

	// HasDualCert returns true if both BLS and RT certificates are present
	HasDualCert() bool

	// BLSSignature returns the aggregated BLS signature
	BLSSignature() []byte

	// RTCertificate returns the Ringtail certificate
	RTCertificate() []byte

	// SetQuantum marks this block as having quantum-secure finality
	SetQuantum() error
}

// QuasarVertex extends Vertex with quantum-secure features
type QuasarVertex interface {
	Vertex

	// HasDualCert returns true if both BLS and RT certificates are present
	HasDualCert() bool

	// BLSSignature returns the aggregated BLS signature
	BLSSignature() []byte

	// RTCertificate returns the Ringtail certificate
	RTCertificate() []byte

	// SetQuantum marks this vertex as having quantum-secure finality
	SetQuantum() error
}