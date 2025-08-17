// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchmarks

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/compression"
)

// BenchmarkMessageCompression benchmarks message compression
func BenchmarkMessageCompression(b *testing.B) {
	// Create sample message data
	data := make([]byte, 4096) // 4KB message
	for i := range data {
		// Create somewhat compressible data
		data[i] = byte(i % 64)
	}

	b.Run("Gzip", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))

		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			w := gzip.NewWriter(&buf)
			_, _ = w.Write(data)
			_ = w.Close()
		}
	})

	b.Run("Zstd", func(b *testing.B) {
		compressor, err := compression.NewZstdCompressor(10 * 1024 * 1024) // 10MB max
		if err != nil {
			b.Skip("Zstd not available")
		}
		b.ResetTimer()
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))

		for i := 0; i < b.N; i++ {
			_, _ = compressor.Compress(data)
		}
	})

	b.Run("NoCompression", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))

		for i := 0; i < b.N; i++ {
			dst := make([]byte, len(data))
			copy(dst, data)
		}
	})
}

// BenchmarkMessageDecompression benchmarks message decompression
func BenchmarkMessageDecompression(b *testing.B) {
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i % 64)
	}

	// Pre-compress data
	var gzipBuf bytes.Buffer
	w := gzip.NewWriter(&gzipBuf)
	_, _ = w.Write(data)
	_ = w.Close()
	gzipData := gzipBuf.Bytes()

	compressor, _ := compression.NewZstdCompressor(10 * 1024 * 1024) // 10MB max
	zstdData, _ := compressor.Compress(data)

	b.Run("Gzip", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))

		for i := 0; i < b.N; i++ {
			r, _ := gzip.NewReader(bytes.NewReader(gzipData))
			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)
			_ = r.Close()
		}
	})

	b.Run("Zstd", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))

		for i := 0; i < b.N; i++ {
			_, _ = compressor.Decompress(zstdData)
		}
	})
}

// BenchmarkMessageSerialization benchmarks message serialization
func BenchmarkNetworkMessageSerialization(b *testing.B) {
	type networkMessage struct {
		ChainID   ids.ID
		RequestID uint32
		NodeID    ids.NodeID
		Data      []byte
	}

	msg := networkMessage{
		ChainID:   ids.GenerateTestID(),
		RequestID: 12345,
		NodeID:    ids.GenerateTestNodeID(),
		Data:      make([]byte, 256),
	}

	b.Run("Manual", func(b *testing.B) {
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			buf := make([]byte, 0, 32+4+32+256)
			buf = append(buf, msg.ChainID[:]...)
			reqIDBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(reqIDBytes, msg.RequestID)
			buf = append(buf, reqIDBytes...)
			buf = append(buf, msg.NodeID[:]...)
			buf = append(buf, msg.Data...)
		}
	})

	b.Run("Buffer", func(b *testing.B) {
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			buf.Write(msg.ChainID[:])
			binary.Write(&buf, binary.BigEndian, msg.RequestID)
			buf.Write(msg.NodeID[:])
			buf.Write(msg.Data)
			_ = buf.Bytes()
		}
	})
}

// BenchmarkNodeIDOperations benchmarks NodeID operations
func BenchmarkNodeIDOperations(b *testing.B) {
	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()

	b.Run("Compare", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = bytes.Compare(nodeID1[:], nodeID2[:])
		}
	})

	b.Run("String", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = nodeID1.String()
		}
	})

	b.Run("MarshalJSON", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = nodeID1.MarshalJSON()
		}
	})
}

// BenchmarkPeerSet benchmarks peer set operations
func BenchmarkPeerSet(b *testing.B) {
	peers := make(map[ids.NodeID]struct{})
	nodeIDs := make([]ids.NodeID, 100)
	for i := range nodeIDs {
		nodeIDs[i] = ids.GenerateTestNodeID()
	}

	b.Run("Add", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			peers[nodeIDs[i%100]] = struct{}{}
		}
	})

	b.Run("Contains", func(b *testing.B) {
		// Pre-populate
		for _, id := range nodeIDs {
			peers[id] = struct{}{}
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, ok := peers[nodeIDs[i%100]]
			_ = ok
		}
	})

	b.Run("Remove", func(b *testing.B) {
		// Pre-populate
		for _, id := range nodeIDs {
			peers[id] = struct{}{}
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			delete(peers, nodeIDs[i%100])
			peers[nodeIDs[i%100]] = struct{}{} // Add it back
		}
	})

	b.Run("Iterate", func(b *testing.B) {
		// Pre-populate
		for _, id := range nodeIDs {
			peers[id] = struct{}{}
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			count := 0
			for range peers {
				count++
			}
		}
	})
}

// BenchmarkMessageQueue benchmarks message queue operations
func BenchmarkMessageQueue(b *testing.B) {
	type message struct {
		ID   ids.ID
		Data []byte
	}

	b.Run("Channel", func(b *testing.B) {
		ch := make(chan message, 100)
		msg := message{
			ID:   ids.GenerateTestID(),
			Data: make([]byte, 256),
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			select {
			case ch <- msg:
			default:
				<-ch
				ch <- msg
			}
		}
	})

	b.Run("BufferedChannel", func(b *testing.B) {
		ch := make(chan message, 1000)
		msg := message{
			ID:   ids.GenerateTestID(),
			Data: make([]byte, 256),
		}

		// Pre-fill to 50%
		for i := 0; i < 500; i++ {
			ch <- msg
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			select {
			case ch <- msg:
				<-ch // Keep it balanced
			default:
			}
		}
	})

	b.Run("Slice", func(b *testing.B) {
		queue := make([]message, 0, 1000)
		msg := message{
			ID:   ids.GenerateTestID(),
			Data: make([]byte, 256),
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			if len(queue) < cap(queue) {
				queue = append(queue, msg)
			} else {
				// Remove first element and append
				queue = queue[1:]
				queue = append(queue, msg)
			}
		}
	})
}

// BenchmarkConnectionPool benchmarks connection pool operations
func BenchmarkConnectionPool(b *testing.B) {
	type connection struct {
		nodeID    ids.NodeID
		connected bool
		data      []byte
	}

	pool := make(map[ids.NodeID]*connection)
	nodeIDs := make([]ids.NodeID, 100)
	for i := range nodeIDs {
		nodeIDs[i] = ids.GenerateTestNodeID()
	}

	b.Run("Get", func(b *testing.B) {
		// Pre-populate
		for _, id := range nodeIDs {
			pool[id] = &connection{
				nodeID:    id,
				connected: true,
				data:      make([]byte, 256),
			}
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			conn := pool[nodeIDs[i%100]]
			_ = conn
		}
	})

	b.Run("Add", func(b *testing.B) {
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			pool[nodeIDs[i%100]] = &connection{
				nodeID:    nodeIDs[i%100],
				connected: true,
				data:      make([]byte, 256),
			}
		}
	})

	b.Run("Remove", func(b *testing.B) {
		// Pre-populate
		for _, id := range nodeIDs {
			pool[id] = &connection{
				nodeID:    id,
				connected: true,
				data:      make([]byte, 256),
			}
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			id := nodeIDs[i%100]
			delete(pool, id)
			// Add it back
			pool[id] = &connection{
				nodeID:    id,
				connected: true,
				data:      make([]byte, 256),
			}
		}
	})
}