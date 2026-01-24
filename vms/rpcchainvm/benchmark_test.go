//go:build grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"testing"
	"time"

	zapwire "github.com/luxfi/api/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	vmpb "github.com/luxfi/node/proto/pb/vm"
)

// Block sizes to test
var blockSizes = []int{
	1024,    // 1KB - small block
	10240,   // 10KB - typical block
	102400,  // 100KB - large block
	1048576, // 1MB - very large block
}

func makeBlockBytes(size int) []byte {
	b := make([]byte, size)
	for i := range b {
		b[i] = byte(i % 256)
	}
	return b
}

func makeBlockID() []byte {
	id := make([]byte, 32)
	for i := range id {
		id[i] = byte(i)
	}
	return id
}

// BenchmarkGetBlockResponse_Protobuf benchmarks protobuf encoding/decoding
func BenchmarkGetBlockResponse_Protobuf(b *testing.B) {
	for _, size := range blockSizes {
		blockBytes := makeBlockBytes(size)
		parentID := makeBlockID()
		ts := timestamppb.New(time.Now())

		b.Run(formatSize(size)+"/Encode", func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				resp := &vmpb.GetBlockResponse{
					ParentId:  parentID,
					Bytes:     blockBytes,
					Height:    12345,
					Timestamp: ts,
				}
				_, err := proto.Marshal(resp)
				if err != nil {
					b.Fatal(err)
				}
			}
		})

		// Pre-encode for decode benchmark
		resp := &vmpb.GetBlockResponse{
			ParentId:  parentID,
			Bytes:     blockBytes,
			Height:    12345,
			Timestamp: ts,
		}
		encoded, _ := proto.Marshal(resp)

		b.Run(formatSize(size)+"/Decode", func(b *testing.B) {
			b.SetBytes(int64(len(encoded)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var decoded vmpb.GetBlockResponse
				if err := proto.Unmarshal(encoded, &decoded); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkGetBlockResponse_ZAP benchmarks ZAP encoding/decoding
func BenchmarkGetBlockResponse_ZAP(b *testing.B) {
	for _, size := range blockSizes {
		blockBytes := makeBlockBytes(size)
		id := makeBlockID()
		parentID := makeBlockID()

		b.Run(formatSize(size)+"/Encode", func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				resp := &zapwire.BlockResponse{
					ID:        id,
					ParentID:  parentID,
					Height:    12345,
					Timestamp: 1234567890,
					Bytes:     blockBytes,
				}
				buf := zapwire.GetBuffer()
				resp.Encode(buf)
				_ = buf.Bytes()
				zapwire.PutBuffer(buf)
			}
		})

		// Pre-encode for decode benchmark
		resp := &zapwire.BlockResponse{
			ID:        id,
			ParentID:  parentID,
			Height:    12345,
			Timestamp: 1234567890,
			Bytes:     blockBytes,
		}
		buf := zapwire.GetBuffer()
		resp.Encode(buf)
		encoded := make([]byte, len(buf.Bytes()))
		copy(encoded, buf.Bytes())
		zapwire.PutBuffer(buf)

		b.Run(formatSize(size)+"/Decode", func(b *testing.B) {
			b.SetBytes(int64(len(encoded)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var decoded zapwire.BlockResponse
				reader := zapwire.NewReader(encoded)
				if err := decoded.Decode(reader); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkBatchedParseBlock_Protobuf benchmarks batched block parsing with protobuf
func BenchmarkBatchedParseBlock_Protobuf(b *testing.B) {
	const batchSize = 10
	const blockSize = 10240 // 10KB blocks
	ts := timestamppb.New(time.Now())

	blocks := make([][]byte, batchSize)
	for i := range blocks {
		blocks[i] = makeBlockBytes(blockSize)
	}

	b.Run("Encode", func(b *testing.B) {
		b.SetBytes(int64(batchSize * blockSize))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			resp := &vmpb.BatchedParseBlockResponse{
				Response: make([]*vmpb.ParseBlockResponse, batchSize),
			}
			for j := 0; j < batchSize; j++ {
				resp.Response[j] = &vmpb.ParseBlockResponse{
					Id:        makeBlockID(),
					ParentId:  makeBlockID(),
					Height:    uint64(j),
					Timestamp: ts,
				}
			}
			_, err := proto.Marshal(resp)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	// Pre-encode for decode benchmark
	resp := &vmpb.BatchedParseBlockResponse{
		Response: make([]*vmpb.ParseBlockResponse, batchSize),
	}
	for j := 0; j < batchSize; j++ {
		resp.Response[j] = &vmpb.ParseBlockResponse{
			Id:        makeBlockID(),
			ParentId:  makeBlockID(),
			Height:    uint64(j),
			Timestamp: ts,
		}
	}
	encoded, _ := proto.Marshal(resp)

	b.Run("Decode", func(b *testing.B) {
		b.SetBytes(int64(len(encoded)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var decoded vmpb.BatchedParseBlockResponse
			if err := proto.Unmarshal(encoded, &decoded); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkBatchedParseBlock_ZAP benchmarks batched block parsing with ZAP
func BenchmarkBatchedParseBlock_ZAP(b *testing.B) {
	const batchSize = 10
	const blockSize = 10240 // 10KB blocks

	blocks := make([][]byte, batchSize)
	for i := range blocks {
		blocks[i] = makeBlockBytes(blockSize)
	}

	b.Run("Encode", func(b *testing.B) {
		b.SetBytes(int64(batchSize * blockSize))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			resp := &zapwire.BatchedParseBlockResponse{
				Responses: make([]zapwire.BlockResponse, batchSize),
			}
			for j := 0; j < batchSize; j++ {
				resp.Responses[j] = zapwire.BlockResponse{
					ID:        makeBlockID(),
					ParentID:  makeBlockID(),
					Height:    uint64(j),
					Timestamp: 1234567890,
					Bytes:     blocks[j],
				}
			}
			buf := zapwire.GetBuffer()
			resp.Encode(buf)
			_ = buf.Bytes()
			zapwire.PutBuffer(buf)
		}
	})

	// Pre-encode for decode benchmark
	resp := &zapwire.BatchedParseBlockResponse{
		Responses: make([]zapwire.BlockResponse, batchSize),
	}
	for j := 0; j < batchSize; j++ {
		resp.Responses[j] = zapwire.BlockResponse{
			ID:        makeBlockID(),
			ParentID:  makeBlockID(),
			Height:    uint64(j),
			Timestamp: 1234567890,
			Bytes:     blocks[j],
		}
	}
	buf := zapwire.GetBuffer()
	resp.Encode(buf)
	encoded := make([]byte, len(buf.Bytes()))
	copy(encoded, buf.Bytes())
	zapwire.PutBuffer(buf)

	b.Run("Decode", func(b *testing.B) {
		b.SetBytes(int64(len(encoded)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var decoded zapwire.BatchedParseBlockResponse
			reader := zapwire.NewReader(encoded)
			if err := decoded.Decode(reader); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkInitializeRequest compares Initialize request serialization
func BenchmarkInitializeRequest_Protobuf(b *testing.B) {
	genesis := makeBlockBytes(65536) // 64KB genesis
	upgrade := makeBlockBytes(1024)  // 1KB upgrade
	config := makeBlockBytes(4096)   // 4KB config

	b.Run("Encode", func(b *testing.B) {
		b.SetBytes(int64(len(genesis) + len(upgrade) + len(config)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			req := &vmpb.InitializeRequest{
				NetworkId:    96369,
				ChainId:      makeBlockID(),
				NodeId:       makeBlockID(),
				GenesisBytes: genesis,
				UpgradeBytes: upgrade,
				ConfigBytes:  config,
			}
			_, err := proto.Marshal(req)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkInitializeRequest_ZAP(b *testing.B) {
	genesis := makeBlockBytes(65536) // 64KB genesis
	upgrade := makeBlockBytes(1024)  // 1KB upgrade
	config := makeBlockBytes(4096)   // 4KB config

	b.Run("Encode", func(b *testing.B) {
		b.SetBytes(int64(len(genesis) + len(upgrade) + len(config)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			req := &zapwire.InitializeRequest{
				NetworkID:    96369,
				ChainID:      makeBlockID(),
				NodeID:       makeBlockID(),
				GenesisBytes: genesis,
				UpgradeBytes: upgrade,
				ConfigBytes:  config,
			}
			buf := zapwire.GetBuffer()
			req.Encode(buf)
			_ = buf.Bytes()
			zapwire.PutBuffer(buf)
		}
	})
}

func formatSize(bytes int) string {
	switch {
	case bytes >= 1048576:
		return "1MB"
	case bytes >= 102400:
		return "100KB"
	case bytes >= 10240:
		return "10KB"
	default:
		return "1KB"
	}
}
