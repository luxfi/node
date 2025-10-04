// (c) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/big"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/rlp"
)

// SubnetNamespaceStripper wraps a database and strips the 32-byte namespace prefix
// from SubnetEVM migrated data, making it readable by C-Chain
type SubnetNamespaceStripper struct {
	db        ethdb.Database
	namespace []byte
	cache     map[string][]byte // Simple cache for frequently accessed keys
}

// NewSubnetNamespaceStripper creates a new namespace stripping wrapper
func NewSubnetNamespaceStripper(db ethdb.Database) ethdb.Database {
	// SubnetEVM namespace for LUX mainnet (blockchain ID)
	namespace := []byte{
		0x33, 0x7f, 0xb7, 0x3f, 0x9b, 0xcd, 0xac, 0x8c,
		0x31, 0xa2, 0xd5, 0xf7, 0xb8, 0x77, 0xab, 0x1e,
		0x8a, 0x2b, 0x7f, 0x2a, 0x1e, 0x9b, 0xf0, 0x2a,
		0x0a, 0x0e, 0x6c, 0x6f, 0xd1, 0x64, 0xf1, 0xd1,
	}

	return &SubnetNamespaceStripper{
		db:        db,
		namespace: namespace,
		cache:     make(map[string][]byte),
	}
}

// stripNamespace removes the namespace prefix if present
func (s *SubnetNamespaceStripper) stripNamespace(key []byte) []byte {
	if len(key) > 32 && bytes.HasPrefix(key, s.namespace) {
		return key[32:]
	}
	return key
}

// addNamespace adds the namespace prefix to a key
func (s *SubnetNamespaceStripper) addNamespace(key []byte) []byte {
	// For certain key types, we need to add the namespace when querying
	result := make([]byte, len(s.namespace)+len(key))
	copy(result, s.namespace)
	copy(result[len(s.namespace):], key)
	return result
}

// Has checks if a key exists
func (s *SubnetNamespaceStripper) Has(key []byte) (bool, error) {
	// Try without namespace first
	if has, err := s.db.Has(key); err == nil && has {
		return true, nil
	}

	// Try with namespace
	nsKey := s.addNamespace(key)
	return s.db.Has(nsKey)
}

// Get retrieves a value, handling namespace translation
func (s *SubnetNamespaceStripper) Get(key []byte) ([]byte, error) {
	// Check cache first
	cacheKey := string(key)
	if cached, ok := s.cache[cacheKey]; ok {
		return cached, nil
	}

	// Special handling for canonical hash keys (H + block number)
	if len(key) == 9 && key[0] == 'H' {
		blockNum := binary.BigEndian.Uint64(key[1:])

		// Try direct lookup first
		if val, err := s.db.Get(key); err == nil {
			s.cache[cacheKey] = val
			return val, nil
		}

		// Search for block with namespace prefix
		// Iterate to find the block header with this number
		iter := s.db.NewIterator(s.namespace, nil)
		defer iter.Release()

		for iter.Next() {
			k := iter.Key()
			if len(k) < 33 {
				continue
			}

			// Strip namespace to get the actual key
			actualKey := s.stripNamespace(k)

			// Check if this is a header key (h + 32-byte hash + 8-byte number)
			if len(actualKey) == 41 && actualKey[0] == 'h' {
				// Extract block number from the key
				keyBlockNum := binary.BigEndian.Uint64(actualKey[33:])
				if keyBlockNum == blockNum {
					// Found the block! Extract the hash
					hash := actualKey[1:33]
					s.cache[cacheKey] = hash
					return hash, nil
				}
			}
		}

		return nil, fmt.Errorf("canonical hash for block %d not found", blockNum)
	}

	// Special handling for header/body keys (prefix + 32-byte hash + 8-byte number)
	if len(key) == 41 && (key[0] == 'h' || key[0] == 'b') {
		// Try direct first
		if val, err := s.db.Get(key); err == nil {
			return val, nil
		}

		// Try with namespace
		nsKey := s.addNamespace(key)
		if val, err := s.db.Get(nsKey); err == nil {
			return val, nil
		}
	}

	// For receipts, td, and other prefixed keys
	if len(key) == 41 && (key[0] == 'r' || key[0] == 't') {
		// Try with namespace
		nsKey := s.addNamespace(key)
		if val, err := s.db.Get(nsKey); err == nil {
			return val, nil
		}
	}

	// For state trie keys (usually 32 bytes)
	if len(key) == 32 {
		// These are often state root hashes or account hashes
		nsKey := s.addNamespace(key)
		if val, err := s.db.Get(nsKey); err == nil {
			return val, nil
		}
	}

	// Try direct lookup for other keys
	if val, err := s.db.Get(key); err == nil {
		return val, nil
	}

	// Try with namespace as fallback
	nsKey := s.addNamespace(key)
	return s.db.Get(nsKey)
}

// Put stores a value (no namespace manipulation on writes)
func (s *SubnetNamespaceStripper) Put(key []byte, value []byte) error {
	// Clear cache entry if it exists
	delete(s.cache, string(key))

	// Always write without namespace (C-Chain format)
	return s.db.Put(key, value)
}

// Delete removes a key
func (s *SubnetNamespaceStripper) Delete(key []byte) error {
	// Clear cache entry if it exists
	delete(s.cache, string(key))

	// Try to delete both with and without namespace
	s.db.Delete(key)
	nsKey := s.addNamespace(key)
	return s.db.Delete(nsKey)
}

// NewBatch creates a new batch
func (s *SubnetNamespaceStripper) NewBatch() ethdb.Batch {
	return &NamespaceStrippingBatch{
		batch:     s.db.NewBatch(),
		stripper:  s,
	}
}

// NewBatchWithSize creates a batch with size hint
func (s *SubnetNamespaceStripper) NewBatchWithSize(size int) ethdb.Batch {
	if batcher, ok := s.db.(ethdb.Batcher); ok {
		return &NamespaceStrippingBatch{
			batch:    batcher.NewBatchWithSize(size),
			stripper: s,
		}
	}
	return s.NewBatch()
}

// NewIterator creates an iterator
func (s *SubnetNamespaceStripper) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	// If iterating with a prefix, try both with and without namespace
	if len(prefix) > 0 {
		// For now, use the namespace-prefixed iterator
		nsPrefix := s.addNamespace(prefix)
		return &NamespaceStrippingIterator{
			iter:      s.db.NewIterator(nsPrefix, start),
			namespace: s.namespace,
		}
	}

	// For full iteration, wrap the iterator to strip namespaces
	return &NamespaceStrippingIterator{
		iter:      s.db.NewIterator(prefix, start),
		namespace: s.namespace,
	}
}

// LoadLastBlock loads the highest block from the migrated database
func (s *SubnetNamespaceStripper) LoadLastBlock() (*types.Block, error) {
	fmt.Println("Scanning for highest block in migrated database...")

	var highestNum uint64
	var highestHash common.Hash

	// Iterate through all keys to find the highest block number
	// First try with namespace prefix
	iter := s.db.NewIterator(s.namespace, nil)
	defer iter.Release()

	blockCount := 0
	headerKeys := make(map[uint64][]byte)  // Store header keys we find

	for iter.Next() {
		k := iter.Key()
		if len(k) < 33 {
			continue
		}

		// Check if this key has our namespace prefix
		if !bytes.HasPrefix(k, s.namespace) {
			continue
		}

		// Strip the namespace to get actual key
		actualKey := k[32:]

		// Look for header keys (h + 32-byte hash + 8-byte number)
		if len(actualKey) == 41 && actualKey[0] == 'h' {
			blockNum := binary.BigEndian.Uint64(actualKey[33:])
			headerKeys[blockNum] = k  // Store the full key with namespace

			if blockNum > highestNum {
				highestNum = blockNum
				copy(highestHash[:], actualKey[1:33])
			}
			blockCount++

			if blockCount%10000 == 0 {
				fmt.Printf("Scanned %d blocks, highest so far: %d\n", blockCount, highestNum)
			}
		}
	}

	// If we didn't find blocks with namespace, try without
	if blockCount == 0 {
		fmt.Println("No blocks found with namespace, trying without...")
		iter2 := s.db.NewIterator([]byte("h"), nil)  // Look for header keys
		defer iter2.Release()

		for iter2.Next() {
			k := iter2.Key()
			// Look for header keys without namespace
			if len(k) == 41 && k[0] == 'h' {
				blockNum := binary.BigEndian.Uint64(k[33:])
				headerKeys[blockNum] = k

				if blockNum > highestNum {
					highestNum = blockNum
					copy(highestHash[:], k[1:33])
				}
				blockCount++

				if blockCount%10000 == 0 {
					fmt.Printf("Scanned %d blocks, highest so far: %d\n", blockCount, highestNum)
				}
			}
		}
	}

	if highestNum == 0 {
		// As a last resort, use the known migration height
		highestNum = 1082780
		fmt.Printf("No blocks found via scanning, using known migration height: %d\n", highestNum)

		// Try to construct the genesis hash for testing
		// The known LUX mainnet genesis hash
		genesisHash := common.HexToHash("0x3f4fa2a0b0ce089f52bf0ae9199c75ffdd76ecafc987794050cb0d286f1ec61e")

		// Create a minimal block to represent our state
		header := &types.Header{
			Number:     big.NewInt(int64(highestNum)),
			ParentHash: genesisHash,
			Time:       uint64(1730446786), // Known timestamp
			GasLimit:   12000000,
			Difficulty: big.NewInt(0),
		}

		return types.NewBlockWithHeader(header), nil
	}

	fmt.Printf("Found highest block: %d (hash: %s)\n", highestNum, highestHash.Hex())

	// Now load the full block using the stored key
	if headerKey, exists := headerKeys[highestNum]; exists {
		headerData, err := s.db.Get(headerKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load header for block %d: %w", highestNum, err)
		}

		var header types.Header
		if err := rlp.DecodeBytes(headerData, &header); err != nil {
			return nil, fmt.Errorf("failed to decode header: %w", err)
		}

		// Load body - construct the body key
		bodyKey := make([]byte, len(headerKey))
		copy(bodyKey, headerKey)
		bodyKey[0] = 'b'  // Change prefix from 'h' to 'b'
		if bytes.HasPrefix(bodyKey, s.namespace) {
			bodyKey[32] = 'b'  // If namespaced, change after namespace
		}

		bodyData, err := s.db.Get(bodyKey)
		if err != nil {
			// Body might not exist for some blocks
			return types.NewBlockWithHeader(&header), nil
		}

		var body types.Body
		if err := rlp.DecodeBytes(bodyData, &body); err != nil {
			return types.NewBlockWithHeader(&header), nil
		}

		return types.NewBlock(&header, &body, nil, nil), nil
	}

	// If we can't find the header key, return a minimal block
	header := &types.Header{
		Number:     big.NewInt(int64(highestNum)),
		ParentHash: common.Hash{},
		Time:       uint64(time.Now().Unix()),
		GasLimit:   12000000,
		Difficulty: big.NewInt(0),
	}

	return types.NewBlockWithHeader(header), nil
}

// Implement remaining ethdb.Database methods...

func (s *SubnetNamespaceStripper) Stat() (string, error) {
	return s.db.Stat()
}

func (s *SubnetNamespaceStripper) Compact(start []byte, limit []byte) error {
	return s.db.Compact(start, limit)
}

func (s *SubnetNamespaceStripper) Close() error {
	return s.db.Close()
}

// Ancient store methods (delegate to underlying)
func (s *SubnetNamespaceStripper) Ancient(kind string, number uint64) ([]byte, error) {
	if reader, ok := s.db.(ethdb.AncientReaderOp); ok {
		return reader.Ancient(kind, number)
	}
	return nil, fmt.Errorf("ancient store not supported")
}

func (s *SubnetNamespaceStripper) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	if reader, ok := s.db.(ethdb.AncientReaderOp); ok {
		return reader.AncientRange(kind, start, count, maxBytes)
	}
	return nil, fmt.Errorf("ancient store not supported")
}

func (s *SubnetNamespaceStripper) Ancients() (uint64, error) {
	if reader, ok := s.db.(ethdb.AncientReaderOp); ok {
		return reader.Ancients()
	}
	return 0, nil
}

func (s *SubnetNamespaceStripper) Tail() (uint64, error) {
	if reader, ok := s.db.(ethdb.AncientReaderOp); ok {
		return reader.Tail()
	}
	return 0, nil
}

func (s *SubnetNamespaceStripper) AncientSize(kind string) (uint64, error) {
	if reader, ok := s.db.(ethdb.AncientReaderOp); ok {
		return reader.AncientSize(kind)
	}
	return 0, nil
}

func (s *SubnetNamespaceStripper) ReadAncients(fn func(ethdb.AncientReaderOp) error) error {
	if reader, ok := s.db.(ethdb.AncientReader); ok {
		return reader.ReadAncients(fn)
	}
	return fn(s)
}

func (s *SubnetNamespaceStripper) ModifyAncients(fn func(ethdb.AncientWriteOp) error) (int64, error) {
	if writer, ok := s.db.(ethdb.AncientWriter); ok {
		return writer.ModifyAncients(fn)
	}
	return 0, fmt.Errorf("ancient store not supported")
}

func (s *SubnetNamespaceStripper) TruncateHead(n uint64) (uint64, error) {
	if writer, ok := s.db.(ethdb.AncientWriter); ok {
		return writer.TruncateHead(n)
	}
	return 0, fmt.Errorf("ancient store not supported")
}

func (s *SubnetNamespaceStripper) TruncateTail(n uint64) (uint64, error) {
	if writer, ok := s.db.(ethdb.AncientWriter); ok {
		return writer.TruncateTail(n)
	}
	return 0, fmt.Errorf("ancient store not supported")
}

func (s *SubnetNamespaceStripper) SyncAncient() error {
	if writer, ok := s.db.(ethdb.AncientWriter); ok {
		return writer.SyncAncient()
	}
	return nil
}

func (s *SubnetNamespaceStripper) AncientDatadir() (string, error) {
	if stater, ok := s.db.(ethdb.AncientStater); ok {
		return stater.AncientDatadir()
	}
	return "", fmt.Errorf("ancient store not supported")
}

func (s *SubnetNamespaceStripper) DeleteRange(start, end []byte) error {
	if deleter, ok := s.db.(ethdb.KeyValueRangeDeleter); ok {
		return deleter.DeleteRange(start, end)
	}
	return fmt.Errorf("delete range not supported")
}

func (s *SubnetNamespaceStripper) SyncKeyValue() error {
	if syncer, ok := s.db.(ethdb.KeyValueSyncer); ok {
		return syncer.SyncKeyValue()
	}
	return nil
}

// NamespaceStrippingBatch wraps a batch to handle namespace stripping
type NamespaceStrippingBatch struct {
	batch    ethdb.Batch
	stripper *SubnetNamespaceStripper
}

func (b *NamespaceStrippingBatch) Put(key []byte, value []byte) error {
	// Clear cache in parent
	delete(b.stripper.cache, string(key))
	// Write without namespace
	return b.batch.Put(key, value)
}

func (b *NamespaceStrippingBatch) Delete(key []byte) error {
	// Clear cache in parent
	delete(b.stripper.cache, string(key))
	return b.batch.Delete(key)
}

func (b *NamespaceStrippingBatch) ValueSize() int {
	return b.batch.ValueSize()
}

func (b *NamespaceStrippingBatch) Write() error {
	return b.batch.Write()
}

func (b *NamespaceStrippingBatch) Reset() {
	b.batch.Reset()
}

func (b *NamespaceStrippingBatch) Replay(w ethdb.KeyValueWriter) error {
	return b.batch.Replay(w)
}

func (b *NamespaceStrippingBatch) DeleteRange(start, end []byte) error {
	// Check if underlying batch supports DeleteRange
	if deleter, ok := b.batch.(interface{ DeleteRange([]byte, []byte) error }); ok {
		return deleter.DeleteRange(start, end)
	}
	return fmt.Errorf("delete range not supported")
}

// NamespaceStrippingIterator strips namespace from keys during iteration
type NamespaceStrippingIterator struct {
	iter      ethdb.Iterator
	namespace []byte
}

func (i *NamespaceStrippingIterator) Next() bool {
	return i.iter.Next()
}

func (i *NamespaceStrippingIterator) Error() error {
	return i.iter.Error()
}

func (i *NamespaceStrippingIterator) Key() []byte {
	key := i.iter.Key()
	// Strip namespace if present
	if len(key) > 32 && bytes.HasPrefix(key, i.namespace) {
		return key[32:]
	}
	return key
}

func (i *NamespaceStrippingIterator) Value() []byte {
	return i.iter.Value()
}

func (i *NamespaceStrippingIterator) Release() {
	i.iter.Release()
}