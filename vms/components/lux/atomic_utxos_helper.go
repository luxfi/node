// Copyright (C) 2019-2023, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lux

import (
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/set"
)

// GetAtomicUTXOs returns exported UTXOs such that at least one of the
// addresses in [addrs] is referenced.
//
// Returns at most [limit] UTXOs.
//
// Returns:
// * The fetched UTXOs
// * The address associated with the last UTXO fetched
// * The ID of the last UTXO fetched
// * Any error that may have occurred upstream.
func GetAtomicUTXOs(
	sharedMemory atomic.SharedMemory,
	codec codec.Manager,
	chainID ids.ID,
	addrs set.Set[ids.ShortID],
	startAddr ids.ShortID,
	startUTXOID ids.ID,
	limit int,
) ([]*UTXO, ids.ShortID, ids.ID, error) {
	// Create an AtomicUTXOManager and use it to get the UTXOs
	manager := NewAtomicUTXOManager(sharedMemory, codec)
	return manager.GetAtomicUTXOs(chainID, addrs, startAddr, startUTXOID, limit)
}