// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"encoding/binary"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/rlp"
)

var (
	ErrBlockNotFound          = errors.New("block not found")
	ErrHashVerificationFailed = errors.New("hash verification failed")
	ErrInvalidBlockSequence   = errors.New("invalid block sequence")
)

// TestBlock represents a simplified block for testing
type TestBlock struct {
	Number       uint64
	Hash         common.Hash
	ParentHash   common.Hash
	StateRoot    common.Hash
	Timestamp    uint64
	GasLimit     uint64
	GasUsed      uint64
	BaseFee      *big.Int
	Header       *types.Header
	Transactions []*types.Transaction
	Body         []byte
}

// BlockImporter handles importing blocks into a database
type BlockImporter struct {
	db    database.Database
	path  string
	mu    sync.Mutex
}

// NewBlockImporter creates a new block importer
func NewBlockImporter(path string) *BlockImporter {
	return &BlockImporter{
		db:   memdb.New(),
		path: path,
	}
}

// ImportBlock imports a block into the database
func (i *BlockImporter) ImportBlock(block *TestBlock) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Write header
	headerKey := HeaderKey(block.Number, block.Hash)
	headerRLP, err := rlp.EncodeToBytes(block.Header)
	if err != nil {
		// Use simplified encoding for test blocks without full header
		headerRLP = encodeTestHeader(block)
	}
	if err := i.db.Put(headerKey, headerRLP); err != nil {
		return err
	}

	// Write canonical mapping
	canonKey := CanonicalKey(block.Number)
	if err := i.db.Put(canonKey, block.Hash[:]); err != nil {
		return err
	}

	// Write body if present
	if len(block.Body) > 0 {
		bodyKey := BodyKey(block.Number, block.Hash)
		if err := i.db.Put(bodyKey, block.Body); err != nil {
			return err
		}
	}

	return nil
}

// ImportBlockWithVerification imports a block and verifies its hash
func (i *BlockImporter) ImportBlockWithVerification(block *TestBlock) error {
	// Verify hash
	computedHash := ComputeBlockHash(block)
	if computedHash != block.Hash {
		return ErrHashVerificationFailed
	}

	return i.ImportBlock(block)
}

// GetBlock retrieves a block by number
func (i *BlockImporter) GetBlock(number uint64) (*TestBlock, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Get canonical hash
	canonKey := CanonicalKey(number)
	hashBytes, err := i.db.Get(canonKey)
	if err != nil {
		return nil, ErrBlockNotFound
	}
	hash := common.BytesToHash(hashBytes)

	// Get header
	headerKey := HeaderKey(number, hash)
	headerRLP, err := i.db.Get(headerKey)
	if err != nil {
		return nil, ErrBlockNotFound
	}

	block := &TestBlock{
		Number: number,
		Hash:   hash,
	}

	// Try to decode as full header
	var header types.Header
	if err := rlp.DecodeBytes(headerRLP, &header); err == nil {
		block.Header = &header
		block.ParentHash = header.ParentHash
		block.StateRoot = header.Root
		block.Timestamp = header.Time
		block.GasLimit = header.GasLimit
		block.GasUsed = header.GasUsed
		block.BaseFee = header.BaseFee
	} else {
		// Decode as test header
		decodeTestHeader(headerRLP, block)
	}

	return block, nil
}

// GetBlockHash retrieves the hash for a block number
func (i *BlockImporter) GetBlockHash(number uint64) (common.Hash, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	canonKey := CanonicalKey(number)
	hashBytes, err := i.db.Get(canonKey)
	if err != nil {
		return common.Hash{}, ErrBlockNotFound
	}
	return common.BytesToHash(hashBytes), nil
}

// GetCanonicalHash retrieves the canonical hash for a block number
func (i *BlockImporter) GetCanonicalHash(number uint64) (common.Hash, error) {
	return i.GetBlockHash(number)
}

// Close closes the block importer
func (i *BlockImporter) Close() error {
	if i.db != nil {
		return i.db.Close()
	}
	return nil
}

// CreateTestBlock creates a test block with the given parameters
func CreateTestBlock(number uint64, parentHash common.Hash) *TestBlock {
	timestamp := uint64(time.Now().Unix())

	block := &TestBlock{
		Number:     number,
		ParentHash: parentHash,
		Timestamp:  timestamp,
		GasLimit:   8000000,
		GasUsed:    21000,
		BaseFee:    big.NewInt(1000000000),
		StateRoot:  common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
	}

	// Compute hash
	block.Hash = ComputeBlockHash(block)

	// Create header
	block.Header = &types.Header{
		ParentHash: parentHash,
		Number:     big.NewInt(int64(number)),
		GasLimit:   block.GasLimit,
		GasUsed:    block.GasUsed,
		Time:       timestamp,
		BaseFee:    block.BaseFee,
		Root:       block.StateRoot,
		Difficulty: big.NewInt(0),
	}

	return block
}

// ComputeBlockHash computes a deterministic hash for a test block
func ComputeBlockHash(block *TestBlock) common.Hash {
	hash := common.Hash{}

	// Include block number
	binary.BigEndian.PutUint64(hash[0:8], block.Number)

	// Include parent hash
	for i := 0; i < 8; i++ {
		hash[8+i] = block.ParentHash[i]
	}

	// Include timestamp
	binary.BigEndian.PutUint64(hash[16:24], block.Timestamp)

	// Include gas limit
	binary.BigEndian.PutUint64(hash[24:32], block.GasLimit)

	return hash
}

// encodeTestHeader creates a simplified header encoding for testing
func encodeTestHeader(block *TestBlock) []byte {
	// Simple encoding: number(8) + parentHash(32) + timestamp(8) + gasLimit(8) + stateRoot(32)
	data := make([]byte, 88)
	binary.BigEndian.PutUint64(data[0:8], block.Number)
	copy(data[8:40], block.ParentHash[:])
	binary.BigEndian.PutUint64(data[40:48], block.Timestamp)
	binary.BigEndian.PutUint64(data[48:56], block.GasLimit)
	copy(data[56:88], block.StateRoot[:])
	return data
}

// decodeTestHeader decodes a simplified header encoding
func decodeTestHeader(data []byte, block *TestBlock) {
	if len(data) < 88 {
		return
	}
	block.Number = binary.BigEndian.Uint64(data[0:8])
	copy(block.ParentHash[:], data[8:40])
	block.Timestamp = binary.BigEndian.Uint64(data[40:48])
	block.GasLimit = binary.BigEndian.Uint64(data[48:56])
	copy(block.StateRoot[:], data[56:88])
}

// WriteTestBlocks writes test blocks to a database
func WriteTestBlocks(db database.Database, count int) {
	parentHash := common.Hash{}
	for i := 0; i < count; i++ {
		block := CreateTestBlock(uint64(i), parentHash)

		// Write header
		headerKey := HeaderKey(uint64(i), block.Hash)
		headerData := encodeTestHeader(block)
		db.Put(headerKey, headerData)

		// Write canonical mapping
		canonKey := CanonicalKey(uint64(i))
		db.Put(canonKey, block.Hash[:])

		parentHash = block.Hash
	}
}
