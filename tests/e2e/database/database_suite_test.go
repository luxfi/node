// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

package database_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database/leveldb"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/pebbledb"
	"github.com/luxfi/node/tests/fixture/e2e"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var flagVars *e2e.FlagVars

func init() {
	flagVars = e2e.RegisterFlags()
}

func TestDatabaseE2E(t *testing.T) {
	ginkgo.RunSpecs(t, "Database E2E Test Suite")
}

var _ = ginkgo.Describe("[Database E2E]", func() {
	ginkgo.Context("Cross-implementation compatibility", func() {
		ginkgo.It("Should handle data migration between different backends", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// Test data migration from LevelDB to PebbleDB
			testDataMigration(ctx)
		})

		ginkgo.It("Should maintain consistency across concurrent operations", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// Test concurrent read/write operations
			testConcurrentOperations(ctx)
		})

		ginkgo.It("Should handle large datasets efficiently", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			// Test with large datasets
			testLargeDatasets(ctx)
		})
	})

	ginkgo.Context("Error handling and recovery", func() {
		ginkgo.It("Should recover from corrupt data gracefully", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			testCorruptionRecovery(ctx)
		})

		ginkgo.It("Should handle disk space exhaustion", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			testDiskSpaceExhaustion(ctx)
		})
	})

	ginkgo.Context("Performance benchmarks", func() {
		ginkgo.It("Should meet read performance requirements", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			testReadPerformance(ctx)
		})

		ginkgo.It("Should meet write performance requirements", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			testWritePerformance(ctx)
		})
	})
})

func testDataMigration(ctx context.Context) {
	ginkgo.GinkgoHelper()

	// Create source database (LevelDB)
	sourceDir, err := os.MkdirTemp("", "leveldb-test-*")
	require.NoError(ginkgo.GinkgoT(), err)
	defer os.RemoveAll(sourceDir)
	sourceDB, err := leveldb.New(sourceDir, 1024, 1024, 1024)
	require.NoError(ginkgo.GinkgoT(), err)
	defer sourceDB.Close()

	// Write test data
	testData := generateTestData(1000)
	for key, value := range testData {
		err := sourceDB.Put([]byte(key), value)
		require.NoError(ginkgo.GinkgoT(), err)
	}

	// Create destination database (PebbleDB)
	destDir, err := os.MkdirTemp("", "pebbledb-test-*")
	require.NoError(ginkgo.GinkgoT(), err)
	defer os.RemoveAll(destDir)
	destDB, err := pebbledb.New(destDir, 1024, 1024, "", false)
	require.NoError(ginkgo.GinkgoT(), err)
	defer destDB.Close()

	// Migrate data
	iter := sourceDB.NewIterator()
	defer iter.Release()

	for iter.Next() {
		err := destDB.Put(iter.Key(), iter.Value())
		require.NoError(ginkgo.GinkgoT(), err)
	}
	require.NoError(ginkgo.GinkgoT(), iter.Error())

	// Verify migration
	for key, expectedValue := range testData {
		value, err := destDB.Get([]byte(key))
		require.NoError(ginkgo.GinkgoT(), err)
		require.Equal(ginkgo.GinkgoT(), expectedValue, value)
	}
}

func testConcurrentOperations(ctx context.Context) {
	ginkgo.GinkgoHelper()

	db := memdb.New()
	defer db.Close()

	// Run concurrent writers
	numWriters := 10
	writesPerWriter := 100

	writeDone := make(chan error, numWriters)
	for i := 0; i < numWriters; i++ {
		go func(writerID int) {
			for j := 0; j < writesPerWriter; j++ {
				key := []byte(fmt.Sprintf("writer-%d-key-%d", writerID, j))
				value := []byte(fmt.Sprintf("value-%d-%d", writerID, j))
				if err := db.Put(key, value); err != nil {
					writeDone <- err
					return
				}
			}
			writeDone <- nil
		}(i)
	}

	// Run concurrent readers
	numReaders := 10
	readDone := make(chan error, numReaders)
	for i := 0; i < numReaders; i++ {
		go func(readerID int) {
			for j := 0; j < 100; j++ {
				// Try to read random keys
				key := []byte(fmt.Sprintf("writer-%d-key-%d", readerID%numWriters, j))
				_, _ = db.Get(key) // May or may not exist yet
			}
			readDone <- nil
		}(i)
	}

	// Wait for all operations to complete
	for i := 0; i < numWriters; i++ {
		require.NoError(ginkgo.GinkgoT(), <-writeDone)
	}
	for i := 0; i < numReaders; i++ {
		require.NoError(ginkgo.GinkgoT(), <-readDone)
	}

	// Verify all data is present
	for i := 0; i < numWriters; i++ {
		for j := 0; j < writesPerWriter; j++ {
			key := []byte(fmt.Sprintf("writer-%d-key-%d", i, j))
			expectedValue := []byte(fmt.Sprintf("value-%d-%d", i, j))
			value, err := db.Get(key)
			require.NoError(ginkgo.GinkgoT(), err)
			require.Equal(ginkgo.GinkgoT(), expectedValue, value)
		}
	}
}

func testLargeDatasets(ctx context.Context) {
	ginkgo.GinkgoHelper()

	db := memdb.New()
	defer db.Close()

	// Write 1MB values
	largeValue := make([]byte, 1024*1024) // 1MB
	for i := range largeValue {
		largeValue[i] = byte(i % 256)
	}

	numEntries := 100
	for i := 0; i < numEntries; i++ {
		key := []byte(fmt.Sprintf("large-key-%d", i))
		err := db.Put(key, largeValue)
		require.NoError(ginkgo.GinkgoT(), err)
	}

	// Read back and verify
	for i := 0; i < numEntries; i++ {
		key := []byte(fmt.Sprintf("large-key-%d", i))
		value, err := db.Get(key)
		require.NoError(ginkgo.GinkgoT(), err)
		require.Equal(ginkgo.GinkgoT(), len(largeValue), len(value))
	}
}

func testCorruptionRecovery(ctx context.Context) {
	ginkgo.GinkgoHelper()
	// Test database recovery from corruption
	// This would involve simulating corruption and testing recovery mechanisms
	ginkgo.Skip("Corruption recovery test - implementation pending")
}

func testDiskSpaceExhaustion(ctx context.Context) {
	ginkgo.GinkgoHelper()
	// Test behavior when disk space is exhausted
	// This would involve limiting disk space and testing graceful failure
	ginkgo.Skip("Disk space exhaustion test - implementation pending")
}

func testReadPerformance(ctx context.Context) {
	ginkgo.GinkgoHelper()

	db := memdb.New()
	defer db.Close()

	// Prepare test data
	numKeys := 10000
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("perf-key-%d", i))
		value := []byte(fmt.Sprintf("perf-value-%d", i))
		require.NoError(ginkgo.GinkgoT(), db.Put(key, value))
	}

	// Measure read performance
	start := time.Now()
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("perf-key-%d", i))
		_, err := db.Get(key)
		require.NoError(ginkgo.GinkgoT(), err)
	}
	duration := time.Since(start)

	// Should read 10k keys in under 1000ms (adjusted for CI/slower environments)
	require.Less(ginkgo.GinkgoT(), duration, 1000*time.Millisecond,
		"Read performance too slow: %v for %d keys", duration, numKeys)
}

func testWritePerformance(ctx context.Context) {
	ginkgo.GinkgoHelper()

	db := memdb.New()
	defer db.Close()

	numKeys := 10000
	start := time.Now()

	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("write-key-%d", i))
		value := []byte(fmt.Sprintf("write-value-%d", i))
		require.NoError(ginkgo.GinkgoT(), db.Put(key, value))
	}

	duration := time.Since(start)

	// Should write 10k keys in under 800ms (adjusted for CI/slower environments)
	require.Less(ginkgo.GinkgoT(), duration, 800*time.Millisecond,
		"Write performance too slow: %v for %d keys", duration, numKeys)
}

func generateTestData(count int) map[string][]byte {
	data := make(map[string][]byte, count)
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("test-key-%d", i)
		value := []byte(fmt.Sprintf("test-value-%d-with-some-extra-data-%d", i, time.Now().Unix()))
		data[key] = value
	}
	return data
}
