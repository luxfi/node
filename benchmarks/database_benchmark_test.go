// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchmarks

import (
	"fmt"
	"testing"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/hashing"
)

// BenchmarkMemoryDatabase benchmarks in-memory database operations
func BenchmarkMemoryDatabase(b *testing.B) {
	db := memdb.New()
	defer db.Close()

	key := []byte("test-key")
	value := make([]byte, 1024) // 1KB value
	for i := range value {
		value[i] = byte(i % 256)
	}

	b.Run("Put", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			k := append(key, fmt.Sprintf("-%d", i)...)
			_ = db.Put(k, value)
		}
		b.SetBytes(int64(len(value)))
	})

	b.Run("Get", func(b *testing.B) {
		// Pre-populate database
		for i := 0; i < 1000; i++ {
			k := append(key, fmt.Sprintf("-%d", i)...)
			_ = db.Put(k, value)
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			k := append(key, fmt.Sprintf("-%d", i%1000)...)
			_, _ = db.Get(k)
		}
	})

	b.Run("Delete", func(b *testing.B) {
		// Pre-populate database
		for i := 0; i < b.N; i++ {
			k := append(key, fmt.Sprintf("-del-%d", i)...)
			_ = db.Put(k, value)
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			k := append(key, fmt.Sprintf("-del-%d", i)...)
			_ = db.Delete(k)
		}
	})

	b.Run("Has", func(b *testing.B) {
		// Pre-populate database
		for i := 0; i < 1000; i++ {
			k := append(key, fmt.Sprintf("-%d", i)...)
			_ = db.Put(k, value)
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			k := append(key, fmt.Sprintf("-%d", i%1000)...)
			_, _ = db.Has(k)
		}
	})
}

// BenchmarkPrefixDatabase benchmarks prefix database operations
func BenchmarkPrefixDatabase(b *testing.B) {
	baseDB := memdb.New()
	defer baseDB.Close()

	prefix := []byte("prefix")
	db := prefixdb.New(prefix, baseDB)

	key := []byte("test-key")
	value := make([]byte, 256) // 256 bytes value

	b.Run("Put", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			k := append(key, fmt.Sprintf("-%d", i)...)
			_ = db.Put(k, value)
		}
		b.SetBytes(int64(len(value)))
	})

	b.Run("Get", func(b *testing.B) {
		// Pre-populate
		for i := 0; i < 100; i++ {
			k := append(key, fmt.Sprintf("-%d", i)...)
			_ = db.Put(k, value)
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			k := append(key, fmt.Sprintf("-%d", i%100)...)
			_, _ = db.Get(k)
		}
	})
}

// BenchmarkVersionDatabase benchmarks versioned database operations
func BenchmarkVersionDatabase(b *testing.B) {
	baseDB := memdb.New()
	defer baseDB.Close()

	db := versiondb.New(baseDB)

	key := []byte("test-key")
	value := make([]byte, 256)

	b.Run("Put", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			k := append(key, fmt.Sprintf("-%d", i)...)
			_ = db.Put(k, value)
		}
		b.SetBytes(int64(len(value)))
	})

	b.Run("Commit", func(b *testing.B) {
		// Add some data before each commit
		for i := 0; i < 10; i++ {
			k := append(key, fmt.Sprintf("-pre-%d", i)...)
			_ = db.Put(k, value)
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			k := append(key, fmt.Sprintf("-%d", i)...)
			_ = db.Put(k, value)
			_ = db.Commit()
		}
	})

	b.Run("Abort", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			k := append(key, fmt.Sprintf("-%d", i)...)
			_ = db.Put(k, value)
			db.Abort()
		}
	})
}

// BenchmarkDatabaseBatch benchmarks batch database operations
func BenchmarkDatabaseBatch(b *testing.B) {
	db := memdb.New()
	defer db.Close()

	b.Run("SmallBatch", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			batch := db.NewBatch()
			for j := 0; j < 10; j++ {
				key := []byte(fmt.Sprintf("key-%d-%d", i, j))
				value := []byte(fmt.Sprintf("value-%d-%d", i, j))
				_ = batch.Put(key, value)
			}
			_ = batch.Write()
		}
	})

	b.Run("MediumBatch", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			batch := db.NewBatch()
			for j := 0; j < 100; j++ {
				key := []byte(fmt.Sprintf("key-%d-%d", i, j))
				value := []byte(fmt.Sprintf("value-%d-%d", i, j))
				_ = batch.Put(key, value)
			}
			_ = batch.Write()
		}
	})

	b.Run("LargeBatch", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			batch := db.NewBatch()
			for j := 0; j < 1000; j++ {
				key := []byte(fmt.Sprintf("key-%d-%d", i, j))
				value := []byte(fmt.Sprintf("value-%d-%d", i, j))
				_ = batch.Put(key, value)
			}
			_ = batch.Write()
		}
	})
}

// BenchmarkDatabaseIterator benchmarks database iteration
func BenchmarkDatabaseIterator(b *testing.B) {
	db := memdb.New()
	defer db.Close()

	// Pre-populate database
	numEntries := 10000
	for i := 0; i < numEntries; i++ {
		key := []byte(fmt.Sprintf("key-%08d", i))
		value := []byte(fmt.Sprintf("value-%d", i))
		_ = db.Put(key, value)
	}

	b.Run("FullIteration", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			iter := db.NewIterator()
			count := 0
			for iter.Next() {
				_ = iter.Key()
				_ = iter.Value()
				count++
			}
			iter.Release()
		}
	})

	b.Run("PrefixIteration", func(b *testing.B) {
		prefix := []byte("key-0000")
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			iter := db.NewIteratorWithPrefix(prefix)
			count := 0
			for iter.Next() {
				_ = iter.Key()
				_ = iter.Value()
				count++
			}
			iter.Release()
		}
	})

	b.Run("RangeIteration", func(b *testing.B) {
		start := []byte("key-00001000")
		end := []byte("key-00002000")
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			iter := db.NewIteratorWithStartAndPrefix(start, end)
			count := 0
			for iter.Next() {
				_ = iter.Key()
				_ = iter.Value()
				count++
			}
			iter.Release()
		}
	})
}

// BenchmarkDatabaseConcurrency benchmarks concurrent database access
func BenchmarkDatabaseConcurrency(b *testing.B) {
	db := memdb.New()
	defer db.Close()

	// Pre-populate
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		value := []byte(fmt.Sprintf("value-%d", i))
		_ = db.Put(key, value)
	}

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := []byte(fmt.Sprintf("key-%d", i%1000))
			
			// Mix of operations
			switch i % 3 {
			case 0:
				_, _ = db.Get(key)
			case 1:
				value := []byte(fmt.Sprintf("new-value-%d", i))
				_ = db.Put(key, value)
			case 2:
				_, _ = db.Has(key)
			}
			i++
		}
	})
}

// BenchmarkDatabaseKeyGeneration benchmarks different key generation strategies
func BenchmarkDatabaseKeyGeneration(b *testing.B) {
	b.Run("Sequential", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = []byte(fmt.Sprintf("key-%d", i))
		}
	})

	b.Run("Hash", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			data := fmt.Sprintf("key-%d", i)
			_ = hashing.ComputeHash256([]byte(data))
		}
	})

	b.Run("ID", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			id := ids.GenerateTestID()
			_ = id[:]
		}
	})
}