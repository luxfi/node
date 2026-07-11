// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lux

import (
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/vm/chains/atomic"
)

var _ AtomicUTXOManager = (*atomicUTXOManager)(nil)

type atomicUTXOManager struct {
	sm atomic.SharedMemory
}

// NewAtomicUTXOManager returns an AtomicUTXOManager backed by ZAP-native
// wire bytes in cross-chain shared memory (no codec.Manager). Callers rely
// on this package's init() fx-aware UTXO.Unmarshal dispatch.
func NewAtomicUTXOManager(sm atomic.SharedMemory) AtomicUTXOManager {
	return &atomicUTXOManager{sm: sm}
}

func (a *atomicUTXOManager) GetAtomicUTXOs(
	chainID ids.ID,
	addrs set.Set[ids.ShortID],
	startAddr ids.ShortID,
	startUTXOID ids.ID,
	limit int,
) ([]*UTXO, ids.ShortID, ids.ID, error) {
	addrsList := make([][]byte, addrs.Len())
	i := 0
	for addr := range addrs {
		copied := addr
		addrsList[i] = copied[:]
		i++
	}

	allUTXOBytes, lastAddr, lastUTXO, err := a.sm.Indexed(
		chainID,
		addrsList,
		startAddr.Bytes(),
		startUTXOID[:],
		limit,
	)
	if err != nil {
		return nil, ids.ShortID{}, ids.Empty, fmt.Errorf("error fetching atomic UTXOs: %w", err)
	}

	lastAddrID, err := ids.ToShortID(lastAddr)
	if err != nil {
		lastAddrID = ids.ShortEmpty
	}
	lastUTXOID, err := ids.ToID(lastUTXO)
	if err != nil {
		lastUTXOID = ids.Empty
	}

	utxos := make([]*UTXO, len(allUTXOBytes))
	for i, utxoBytes := range allUTXOBytes {
		utxo := &UTXO{}
		if err := utxo.Unmarshal(utxoBytes); err != nil {
			return nil, ids.ShortID{}, ids.Empty, fmt.Errorf("error parsing UTXO: %w", err)
		}
		utxos[i] = utxo
	}
	return utxos, lastAddrID, lastUTXOID, nil
}

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
	chainID ids.ID,
	addrs set.Set[ids.ShortID],
	startAddr ids.ShortID,
	startUTXOID ids.ID,
	limit int,
) ([]*UTXO, ids.ShortID, ids.ID, error) {
	manager := NewAtomicUTXOManager(sharedMemory)
	return manager.GetAtomicUTXOs(chainID, addrs, startAddr, startUTXOID, limit)
}
