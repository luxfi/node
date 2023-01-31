<<<<<<< HEAD
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
=======
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
>>>>>>> 34554f662 (Update LICENSE)
// See the file LICENSE for licensing terms.

package lux

// Factory returns new instances of Consensus
type Factory interface {
	New() Consensus
}
