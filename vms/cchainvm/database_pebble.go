// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// (c) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/ethdb/pebble"
)

// PebbleWithAncient wraps a PebbleDB database to add Ancient store method stubs
// This allows PebbleDB to be used with geth's blockchain, which expects Ancient methods
type PebbleWithAncient struct {
	*pebble.Database
}

// NewPebbleWithAncient wraps a PebbleDB database with Ancient store stubs
func NewPebbleWithAncient(db *pebble.Database) ethdb.Database {
	return &PebbleWithAncient{Database: db}
}

// Ancient store method stubs (same pattern as BadgerDB)
// These are no-ops since we don't actually use the ancient store with EVM data

func (db *PebbleWithAncient) AncientDatadir() (string, error) {
	return "", nil
}

func (db *PebbleWithAncient) ReadAncients(fn func(ethdb.AncientReaderOp) error) error {
	return nil
}

func (db *PebbleWithAncient) ModifyAncients(fn func(ethdb.AncientWriteOp) error) (int64, error) {
	return 0, nil
}

func (db *PebbleWithAncient) TruncateHead(n uint64) (uint64, error) {
	return 0, nil
}

func (db *PebbleWithAncient) TruncateTail(n uint64) (uint64, error) {
	return 0, nil
}

func (db *PebbleWithAncient) Sync() error {
	return nil
}

func (db *PebbleWithAncient) SyncAncient() error {
	return nil
}

func (db *PebbleWithAncient) SyncKeyValue() error {
	return nil
}

func (db *PebbleWithAncient) AncientOffSet() uint64 {
	return 0
}

func (db *PebbleWithAncient) Ancients() (uint64, error) {
	return 0, nil
}

func (db *PebbleWithAncient) Tail() (uint64, error) {
	return 0, nil
}

func (db *PebbleWithAncient) AncientSize(kind string) (uint64, error) {
	return 0, nil
}

func (db *PebbleWithAncient) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	return nil, nil
}

func (db *PebbleWithAncient) HasAncient(kind string, number uint64) (bool, error) {
	return false, nil
}

func (db *PebbleWithAncient) Ancient(kind string, number uint64) ([]byte, error) {
	return nil, nil
}

func (db *PebbleWithAncient) AncientBytes(kind string, id, offset, length uint64) ([]byte, error) {
	return nil, nil
}

func (db *PebbleWithAncient) AncientBatch() ethdb.AncientWriteOp {
	return nil
}
