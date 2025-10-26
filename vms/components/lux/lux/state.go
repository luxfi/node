<<<<<<< HEAD:vms/components/avax/state.go
// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
=======
// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
>>>>>>> origin/regenesis-runtime-replay:vms/components/lux/lux/state.go
// See the file LICENSE for licensing terms.

package lux

const (
	codecVersion = 0
)

// Addressable is the interface a feature extension must provide to be able to
// be tracked as a part of the utxo set for a set of addresses
type Addressable interface {
	Addresses() [][]byte
}
