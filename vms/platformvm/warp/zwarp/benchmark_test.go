// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zwarp

import (
	"context"
	"testing"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/warp"
)

// benchmarkSigner implements warp.Signer for benchmarking
type benchmarkSigner struct {
	signature []byte
}

func (s *benchmarkSigner) Sign(msg *warp.UnsignedMessage) ([]byte, error) {
	return s.signature, nil
}

func (s *benchmarkSigner) PublicKey() []byte {
	return []byte{1, 2, 3, 4}
}

// BenchmarkZAPRoundTrip benchmarks full client-server round trip using ZAP Server
func BenchmarkZAPRoundTrip(b *testing.B) {
	signer := &benchmarkSigner{
		signature: make([]byte, 96),
	}
	server := NewServer(signer)

	// Create ZAP listener
	zapListener, err := zapwire.Listen("127.0.0.1:0", nil)
	if err != nil {
		b.Fatal(err)
	}
	defer zapListener.Close()

	// Create ZAP server
	zapServer := zapwire.NewServer(zapListener, server.Handler())
	go zapServer.Serve(context.Background())
	defer zapServer.Close()

	// Create client connection
	zapConn, err := zapwire.Dial(context.Background(), zapListener.Addr().String(), nil)
	if err != nil {
		b.Fatal(err)
	}
	defer zapConn.Close()

	client := NewClient(zapConn)

	sourceChainID := ids.GenerateTestID()
	payload := make([]byte, 256)
	msg, err := warp.NewUnsignedMessage(1, sourceChainID, payload)
	if err != nil {
		b.Fatal(err)
	}

	// Warm up
	_, err = client.Sign(msg)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := client.Sign(msg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkZAPServerHandle(b *testing.B) {
	signer := &benchmarkSigner{
		signature: make([]byte, 96),
	}
	server := NewServer(signer)

	sourceChainID := ids.GenerateTestID()
	req := &zapwire.WarpSignRequest{
		NetworkID:     1,
		SourceChainID: sourceChainID[:],
		Payload:       make([]byte, 256),
	}

	buf := zapwire.GetBuffer()
	req.Encode(buf)
	payload := buf.Bytes()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := server.HandleMessage(ctx, zapwire.MsgWarpSign, payload)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkZAPBatchSign(b *testing.B) {
	signer := &benchmarkSigner{
		signature: make([]byte, 96),
	}
	server := NewServer(signer)

	// Create batch of 10 messages
	messages := make([]zapwire.WarpSignRequest, 10)
	for i := range messages {
		sourceChainID := ids.GenerateTestID()
		messages[i] = zapwire.WarpSignRequest{
			NetworkID:     1,
			SourceChainID: sourceChainID[:],
			Payload:       make([]byte, 256),
		}
	}

	req := &zapwire.WarpBatchSignRequest{Messages: messages}
	buf := zapwire.GetBuffer()
	req.Encode(buf)
	payload := buf.Bytes()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := server.HandleMessage(ctx, zapwire.MsgWarpBatchSign, payload)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark encoding/decoding overhead
func BenchmarkZAPEncode(b *testing.B) {
	sourceChainID := ids.GenerateTestID()
	req := &zapwire.WarpSignRequest{
		NetworkID:     1,
		SourceChainID: sourceChainID[:],
		Payload:       make([]byte, 256),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		buf := zapwire.GetBuffer()
		req.Encode(buf)
		zapwire.PutBuffer(buf)
	}
}

func BenchmarkZAPDecode(b *testing.B) {
	sourceChainID := ids.GenerateTestID()
	req := &zapwire.WarpSignRequest{
		NetworkID:     1,
		SourceChainID: sourceChainID[:],
		Payload:       make([]byte, 256),
	}

	buf := zapwire.GetBuffer()
	req.Encode(buf)
	data := buf.Bytes()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		decoded := &zapwire.WarpSignRequest{}
		_ = decoded.Decode(zapwire.NewReader(data))
	}
}

func BenchmarkZAPResponseEncode(b *testing.B) {
	resp := &zapwire.WarpSignResponse{
		Signature: make([]byte, 96),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		buf := zapwire.GetBuffer()
		resp.Encode(buf)
		zapwire.PutBuffer(buf)
	}
}

func BenchmarkZAPBatchEncode(b *testing.B) {
	messages := make([]zapwire.WarpSignRequest, 10)
	for i := range messages {
		sourceChainID := ids.GenerateTestID()
		messages[i] = zapwire.WarpSignRequest{
			NetworkID:     1,
			SourceChainID: sourceChainID[:],
			Payload:       make([]byte, 256),
		}
	}

	req := &zapwire.WarpBatchSignRequest{Messages: messages}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		buf := zapwire.GetBuffer()
		req.Encode(buf)
		zapwire.PutBuffer(buf)
	}
}
