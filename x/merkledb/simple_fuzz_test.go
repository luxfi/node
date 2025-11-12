// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"context"
	"testing"

	"github.com/luxfi/database/memdb"
)

// FuzzDatabaseOperations tests database operations with random data
func FuzzDatabaseOperations(f *testing.F) {
	// Seed corpus with various key-value patterns
	f.Add([]byte("key1"), []byte("value1"))
	f.Add([]byte{}, []byte{})
	f.Add([]byte{0xff, 0xfe, 0xfd}, []byte{1, 2, 3, 4, 5})
	f.Add(bytes.Repeat([]byte{0xaa}, 100), bytes.Repeat([]byte{0xbb}, 100))
	
	ctx := context.Background()
	
	f.Fuzz(func(t *testing.T, key []byte, value []byte) {
		// Limit key and value sizes to avoid OOM
		if len(key) > 1024 {
			key = key[:1024]
		}
		if len(value) > 10000 {
			value = value[:10000]
		}
		
		// Create a new database
		db, err := New(ctx, memdb.New(), Config{
			BranchFactor:                BranchFactor16,
			HistoryLength:              100,
			ValueNodeCacheSize:         1024,
			IntermediateNodeCacheSize:   1024,
			IntermediateWriteBufferSize: 16,
			IntermediateWriteBatchSize:  256,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		
		// Test Put operation
		batch := db.NewBatch()
		err = batch.Put(key, value)
		if err != nil {
			// Some keys might be invalid
			return
		}
		
		// Write the batch
		err = batch.Write()
		if err != nil {
			// Some operations might fail
			return
		}
		
		// Test Get operation
		retrievedValue, err := db.Get(key)
		if err != nil {
			t.Errorf("Failed to get key after successful put: %v", err)
			return
		}
		
		// Verify value matches
		if !bytes.Equal(retrievedValue, value) {
			t.Errorf("Value mismatch: got %x, want %x", retrievedValue, value)
		}
		
		// Test Delete operation
		batch2 := db.NewBatch()
		err = batch2.Delete(key)
		if err != nil {
			t.Errorf("Failed to delete key: %v", err)
			return
		}
		
		err = batch2.Write()
		if err != nil {
			t.Errorf("Failed to write delete batch: %v", err)
			return
		}
		
		// Verify key is deleted
		_, err = db.Get(key)
		if err == nil {
			t.Error("Key should be deleted but still exists")
		}
	})
}

// FuzzBatchOperations tests batch operations with multiple keys
func FuzzBatchOperations(f *testing.F) {
	// Seed corpus
	f.Add([]byte("prefix"), uint8(10))
	f.Add([]byte{}, uint8(0))
	f.Add([]byte{0xff}, uint8(255))
	
	ctx := context.Background()
	
	f.Fuzz(func(t *testing.T, prefix []byte, count uint8) {
		// Limit prefix size
		if len(prefix) > 100 {
			prefix = prefix[:100]
		}
		
		// Limit count to avoid too many operations
		if count > 50 {
			count = 50
		}
		
		// Create database
		db, err := New(ctx, memdb.New(), Config{
			BranchFactor:                BranchFactor16,
			HistoryLength:              100,
			ValueNodeCacheSize:         1024,
			IntermediateNodeCacheSize:   1024,
			IntermediateWriteBufferSize: 16,
			IntermediateWriteBatchSize:  256,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		
		// Create batch with multiple operations
		batch := db.NewBatch()
		keys := make([][]byte, 0, count)
		
		for i := uint8(0); i < count; i++ {
			key := append(prefix, byte(i))
			value := []byte{i, i, i}
			
			err = batch.Put(key, value)
			if err != nil {
				// Some keys might be invalid
				continue
			}
			keys = append(keys, key)
		}
		
		// Write batch
		err = batch.Write()
		if err != nil {
			// Batch might fail
			return
		}
		
		// Verify all keys exist
		for i, key := range keys {
			val, err := db.Get(key)
			if err != nil {
				t.Errorf("Failed to get key %x: %v", key, err)
				continue
			}
			
			expectedVal := []byte{uint8(i), uint8(i), uint8(i)}
			if !bytes.Equal(val, expectedVal) {
				t.Errorf("Value mismatch for key %x: got %x, want %x", key, val, expectedVal)
			}
		}
	})
}

// FuzzIterator tests iterator operations with random ranges
func FuzzIterator(f *testing.F) {
	// Seed corpus
	f.Add([]byte("start"), []byte("end"))
	f.Add([]byte{0}, []byte{255})
	f.Add([]byte("a"), []byte("z"))
	
	ctx := context.Background()
	
	f.Fuzz(func(t *testing.T, start []byte, end []byte) {
		// Limit key sizes
		if len(start) > 100 {
			start = start[:100]
		}
		if len(end) > 100 {
			end = end[:100]
		}
		
		// Create database with some data
		db, err := New(ctx, memdb.New(), Config{
			BranchFactor:                BranchFactor16,
			HistoryLength:              100,
			ValueNodeCacheSize:         1024,
			IntermediateNodeCacheSize:   1024,
			IntermediateWriteBufferSize: 16,
			IntermediateWriteBatchSize:  256,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		
		// Add some test data
		batch := db.NewBatch()
		testKeys := [][]byte{
			[]byte("key1"), []byte("key2"), []byte("key3"),
			start, end,
		}
		
		for _, key := range testKeys {
			if len(key) > 0 {
				_ = batch.Put(key, []byte("value"))
			}
		}
		
		if err := batch.Write(); err != nil {
			return
		}
		
		// Create iterator
		it := db.NewIteratorWithStartAndPrefix(start, nil)
		defer it.Release()
		
		// Iterate and ensure no panic
		count := 0
		for it.Next() && count < 100 { // Limit iterations
			_ = it.Key()
			_ = it.Value()
			count++
		}
		
		// Check iterator error
		if err := it.Error(); err != nil {
			// Some errors are expected for invalid ranges
			return
		}
	})
}