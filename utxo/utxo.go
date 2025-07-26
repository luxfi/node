// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package utxo provides a generic UTXO (Unspent Transaction Output) management system
// that can be used across different blockchain architectures (Bitcoin, XRPL, Lux)
package utxo

import (
	"context"
	"math/big"
	"sync"

	"github.com/luxfi/ids"
)

// ChainType represents different blockchain architectures
type ChainType int

const (
	ChainTypeLux ChainType = iota
	ChainTypeBitcoin
	ChainTypeXRPL
	ChainTypeEVM
)

// UTXO represents a generic unspent transaction output
type UTXO struct {
	ID           ids.ID
	TxID         ids.ID
	OutputIndex  uint32
	AssetID      ids.ID
	Amount       *big.Int
	Address      string // Can be different formats based on chain type
	ScriptPubKey []byte // For Bitcoin-style chains
	Locktime     uint64
	ChainType    ChainType
	Extra        interface{} // Chain-specific data
}

// Manager provides chain-agnostic UTXO management
type Manager interface {
	// AddUTXO adds a new UTXO to the set
	AddUTXO(ctx context.Context, utxo *UTXO) error

	// RemoveUTXO removes a UTXO from the set
	RemoveUTXO(ctx context.Context, utxoID ids.ID) error

	// GetUTXO retrieves a specific UTXO
	GetUTXO(ctx context.Context, utxoID ids.ID) (*UTXO, error)

	// GetUTXOs retrieves all UTXOs for a given address
	GetUTXOs(ctx context.Context, address string) ([]*UTXO, error)

	// GetBalance calculates the total balance for an address
	GetBalance(ctx context.Context, address string, assetID ids.ID) (*big.Int, error)

	// SelectUTXOs selects UTXOs to meet a target amount
	SelectUTXOs(ctx context.Context, address string, assetID ids.ID, targetAmount *big.Int) ([]*UTXO, *big.Int, error)
}

// BaseManager provides a basic implementation of UTXO management
type BaseManager struct {
	mu     sync.RWMutex
	utxos  map[ids.ID]*UTXO
	byAddr map[string][]ids.ID
}

// NewBaseManager creates a new base UTXO manager
func NewBaseManager() *BaseManager {
	return &BaseManager{
		utxos:  make(map[ids.ID]*UTXO),
		byAddr: make(map[string][]ids.ID),
	}
}

func (m *BaseManager) AddUTXO(_ context.Context, utxo *UTXO) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.utxos[utxo.ID] = utxo
	m.byAddr[utxo.Address] = append(m.byAddr[utxo.Address], utxo.ID)
	return nil
}

func (m *BaseManager) RemoveUTXO(_ context.Context, utxoID ids.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	utxo, exists := m.utxos[utxoID]
	if !exists {
		return nil
	}

	delete(m.utxos, utxoID)

	// Remove from address index
	addrUTXOs := m.byAddr[utxo.Address]
	for i, id := range addrUTXOs {
		if id == utxoID {
			m.byAddr[utxo.Address] = append(addrUTXOs[:i], addrUTXOs[i+1:]...)
			break
		}
	}

	return nil
}

func (m *BaseManager) GetUTXO(_ context.Context, utxoID ids.ID) (*UTXO, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	utxo, exists := m.utxos[utxoID]
	if !exists {
		return nil, ErrUTXONotFound
	}
	return utxo, nil
}

func (m *BaseManager) GetUTXOs(_ context.Context, address string) ([]*UTXO, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	utxoIDs := m.byAddr[address]
	result := make([]*UTXO, 0, len(utxoIDs))

	for _, id := range utxoIDs {
		if utxo, exists := m.utxos[id]; exists {
			result = append(result, utxo)
		}
	}

	return result, nil
}

func (m *BaseManager) GetBalance(_ context.Context, address string, assetID ids.ID) (*big.Int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	balance := big.NewInt(0)
	utxoIDs := m.byAddr[address]

	for _, id := range utxoIDs {
		if utxo, exists := m.utxos[id]; exists && utxo.AssetID == assetID {
			balance.Add(balance, utxo.Amount)
		}
	}

	return balance, nil
}

func (m *BaseManager) SelectUTXOs(_ context.Context, address string, assetID ids.ID, targetAmount *big.Int) ([]*UTXO, *big.Int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var selected []*UTXO
	total := big.NewInt(0)

	// Simple greedy selection - can be improved with better algorithms
	for _, id := range m.byAddr[address] {
		if utxo, exists := m.utxos[id]; exists && utxo.AssetID == assetID {
			selected = append(selected, utxo)
			total.Add(total, utxo.Amount)

			if total.Cmp(targetAmount) >= 0 {
				return selected, total, nil
			}
		}
	}

	return nil, nil, ErrInsufficientFunds
}
