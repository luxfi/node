// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// Regenesis - Automatically replays old net/evm blocks at runtime

package cchainvm

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/ethdb/pebble"
	"github.com/luxfi/geth/rlp"
)

// Regenesis handles automatic replay of old net/evm blocks
// Runs in parallel with live C-Chain, both feed into Quasar
type Regenesis struct {
	db        *pebble.Database
	current   uint64
	batchSize uint64
	delay     time.Duration
	onBlock   func(height uint64, hash common.Hash, header *types.Header)
}

// StartRegenesis looks for old net/evm database and starts background replay
// Returns nil if no historic data found (normal operation)
func StartRegenesis(ctx context.Context, onBlock func(uint64, common.Hash, *types.Header)) error {
	// Look for old net/evm databases
	home, err := os.UserHomeDir()
	if err != nil {
		return nil // Non-fatal
	}

	paths := []string{
		filepath.Join(home, "work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb"),
		filepath.Join(home, "work/lux/state/chaindata/lux-testnet-96368/db/pebbledb"),
	}

	var dbPath string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			dbPath = p
			break
		}
	}

	if dbPath == "" {
		return nil // No historic data
	}

	// Open old database (read-only)
	db, err := pebble.New(dbPath, 0, 0, "", true)
	if err != nil {
		return err
	}

	replay := &Regenesis{
		db:        db,
		current:   0,
		batchSize: 100,
		delay:     100 * time.Millisecond,
		onBlock:   onBlock,
	}

	go replay.run(ctx)

	fmt.Printf("[REGENESIS] Historic blocks feeding into Quasar from: %s\n", dbPath)
	return nil
}

// run processes historic blocks in background
func (hr *Regenesis) run(ctx context.Context) {
	defer hr.db.Close()

	ticker := time.NewTicker(hr.delay)
	defer ticker.Stop()

	namespace := common.FromHex("337fb73f9bcdac8c31a2d5f7b877ab1e8a2b7f2a1e9bf02a0a0e6c6fd164f1d1")

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			end := hr.current + hr.batchSize
			processed := 0

			for n := hr.current; n < end; n++ {
				header, hash, err := hr.load(n, namespace)
				if err != nil {
					if n == hr.current {
						// End of historic chain
						fmt.Printf("[REGENESIS] ✅ Complete: %d historic blocks quantum stamped\n", hr.current)
						return
					}
					continue
				}

				if hr.onBlock != nil {
					hr.onBlock(n, hash, header)
				}
				processed++
			}

			if processed > 0 {
				hr.current = end
				if hr.current%1000 == 0 {
					fmt.Printf("[REGENESIS] Block %d stamped\n", hr.current)
				}
			}
		}
	}
}

// load reads a block header from old database
func (hr *Regenesis) load(number uint64, namespace []byte) (*types.Header, common.Hash, error) {
	// Canonical hash key
	canonKey := append(namespace, hr.canonKey(number)...)
	hashBytes, err := hr.db.Get(canonKey)
	if err != nil {
		return nil, common.Hash{}, err
	}

	hash := common.BytesToHash(hashBytes)

	// Header key
	headerKey := append(namespace, hr.headerKey(number, hash)...)
	data, err := hr.db.Get(headerKey)
	if err != nil {
		return nil, common.Hash{}, err
	}

	var header types.Header
	if err := rlp.DecodeBytes(data, &header); err != nil {
		return nil, common.Hash{}, err
	}

	return &header, hash, nil
}

func (hr *Regenesis) canonKey(n uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	return append(append([]byte("h"), b...), 'n')
}

func (hr *Regenesis) headerKey(n uint64, hash common.Hash) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	return append(append([]byte("h"), b...), hash.Bytes()...)
}
