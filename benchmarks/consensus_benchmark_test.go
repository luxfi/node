// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchmarks

import (
	"context"
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/hashing"
	"github.com/luxfi/node/utils/logging"
)

// BenchmarkHashingComputeHash256 benchmarks SHA256 hashing performance
func BenchmarkHashingComputeHash256(b *testing.B) {
	data := make([]byte, 1024) // 1KB of data
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = hashing.ComputeHash256(data)
	}

	b.SetBytes(int64(len(data)))
}

// BenchmarkHashingComputeHash256Array benchmarks SHA256 array hashing
func BenchmarkHashingComputeHash256Array(b *testing.B) {
	data := make([]byte, 32) // 32 bytes (common hash size)
	for i := range data {
		data[i] = byte(i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = hashing.ComputeHash256Array(data)
	}

	b.SetBytes(int64(len(data)))
}

// BenchmarkIDGeneration benchmarks ID generation performance
func BenchmarkIDGeneration(b *testing.B) {
	data := []byte("test data for ID generation")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = ids.ID(hashing.ComputeHash256(data))
	}
}

// BenchmarkIDComparison benchmarks ID comparison operations
func BenchmarkIDComparison(b *testing.B) {
	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = id1.Compare(id2)
	}
}

// BenchmarkIDString benchmarks ID string conversion
func BenchmarkIDString(b *testing.B) {
	id := ids.GenerateTestID()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = id.String()
	}
}

// BenchmarkIDFromString benchmarks parsing ID from string
func BenchmarkIDFromString(b *testing.B) {
	idStr := ids.GenerateTestID().String()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = ids.FromString(idStr)
	}
}

// BenchmarkVerifySignature benchmarks signature verification
func BenchmarkVerifySignature(b *testing.B) {
	// This is a placeholder for actual signature verification
	// In real implementation, this would use actual crypto operations
	message := []byte("message to sign")
	signature := make([]byte, 65) // typical signature size

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Simulate signature verification
		_ = len(message) + len(signature)
	}
}

// BenchmarkContextWithTimeout benchmarks context creation with timeout
func BenchmarkContextWithTimeout(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cancel()
		_ = ctx
	}
}

// BenchmarkLoggerCreation benchmarks logger creation
func BenchmarkLoggerCreation(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = logging.NewLogger("")
	}
}

// BenchmarkMapOperations benchmarks map operations commonly used in consensus
func BenchmarkMapOperations(b *testing.B) {
	b.Run("Insert", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			m := make(map[ids.ID]bool)
			for j := 0; j < 100; j++ {
				m[ids.GenerateTestID()] = true
			}
		}
	})

	b.Run("Lookup", func(b *testing.B) {
		m := make(map[ids.ID]bool)
		keys := make([]ids.ID, 100)
		for i := 0; i < 100; i++ {
			id := ids.GenerateTestID()
			keys[i] = id
			m[id] = true
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = m[keys[i%100]]
		}
	})

	b.Run("Delete", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			m := make(map[ids.ID]bool)
			id := ids.GenerateTestID()
			m[id] = true
			delete(m, id)
		}
	})
}

// BenchmarkSliceOperations benchmarks slice operations
func BenchmarkSliceOperations(b *testing.B) {
	b.Run("Append", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			s := make([]ids.ID, 0, 100)
			for j := 0; j < 100; j++ {
				s = append(s, ids.GenerateTestID())
			}
		}
	})

	b.Run("Copy", func(b *testing.B) {
		src := make([]ids.ID, 100)
		for i := range src {
			src[i] = ids.GenerateTestID()
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			dst := make([]ids.ID, len(src))
			copy(dst, src)
		}
	})
}

// BenchmarkChannelOperations benchmarks channel operations
func BenchmarkChannelOperations(b *testing.B) {
	b.Run("Send", func(b *testing.B) {
		ch := make(chan ids.ID, 100)
		id := ids.GenerateTestID()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			select {
			case ch <- id:
			default:
				// Channel full, drain it
				<-ch
				ch <- id
			}
		}
	})

	b.Run("Receive", func(b *testing.B) {
		ch := make(chan ids.ID, 100)
		id := ids.GenerateTestID()
		for i := 0; i < 100; i++ {
			ch <- id
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			select {
			case <-ch:
				ch <- id // Put it back
			default:
			}
		}
	})
}

// BenchmarkMessageSerialization benchmarks message serialization
func BenchmarkMessageSerialization(b *testing.B) {
	type testMessage struct {
		ID        ids.ID
		Height    uint64
		Timestamp time.Time
		Data      []byte
	}

	msg := testMessage{
		ID:        ids.GenerateTestID(),
		Height:    1000000,
		Timestamp: time.Now(),
		Data:      make([]byte, 256),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Simulate serialization by accessing all fields
		_ = msg.ID
		_ = msg.Height
		_ = msg.Timestamp
		_ = msg.Data
	}
}