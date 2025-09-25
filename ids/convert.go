// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	luxids "github.com/luxfi/ids"
)

// ToLuxID converts a node/ids.ID to luxfi/ids.ID
func ToLuxID(id ID) luxids.ID {
	var luxID luxids.ID
	copy(luxID[:], id[:])
	return luxID
}

// FromLuxID converts a luxfi/ids.ID to node/ids.ID
func FromLuxID(luxID luxids.ID) ID {
	var id ID
	copy(id[:], luxID[:])
	return id
}
