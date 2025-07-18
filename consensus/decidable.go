// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"context"

	"github.com/luxfi/node/ids"
)

// Decidable represents element that can be decided.
//
// Decidable objects are typically thought of as either transactions, blocks, or
// vertices.
type Decidable interface {
	// ID returns a unique ID for this element.
	//
	// Typically, this is implemented by using a cryptographic hash of a
	// binary representation of this element. An element should return the same
	// IDs upon repeated calls.
	ID() ids.ID

	// Accept this element.
	//
	// This element will be accepted by every correct node in the network.
	Accept(context.Context) error

	// Reject this element.
	//
	// This element will not be accepted by any correct node in the network.
	Reject(context.Context) error
}
