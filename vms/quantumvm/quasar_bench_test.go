// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package qvm

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luxfi/consensus/protocol/quasar"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/quantumvm/quantum"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// BENCHMARKS
// =============================================================================

// BenchmarkQuasar_MessageCreation benchmarks finality message creation
func BenchmarkQuasar_MessageCreation(b *testing.B) {
	logger := log.NewLogger("bench")
	q, _ := NewQuasar(logger, 3, 2, 3)

	event := FinalityEvent{
		Height:    12345,
		BlockID:   ids.GenerateTestID(),
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = q.createMessage(event)
	}
}

// BenchmarkQuasar_QuorumCheck benchmarks quorum verification
func BenchmarkQuasar_QuorumCheck(b *testing.B) {
	logger := log.NewLogger("bench")
	q, _ := NewQuasar(logger, 3, 2, 3)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = q.checkQuorum(67, 100)
	}
}

// BenchmarkQuasar_TotalWeight benchmarks total weight calculation
func BenchmarkQuasar_TotalWeight(b *testing.B) {
	logger := log.NewLogger("bench")
	q, _ := NewQuasar(logger, 3, 2, 3)

	validators := make([]ValidatorState, 100)
	for i := range validators {
		validators[i] = ValidatorState{
			Weight: uint64(100 + i),
			Active: i%3 != 0, // 2/3 active
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = q.totalWeight(validators)
	}
}

// BenchmarkQuasar_Stats benchmarks stats retrieval (read lock performance)
func BenchmarkQuasar_Stats(b *testing.B) {
	logger := log.NewLogger("bench")
	q, _ := NewQuasar(logger, 3, 2, 3)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = q.Stats()
	}
}

// BenchmarkQuasar_GetFinality benchmarks finality lookup
func BenchmarkQuasar_GetFinality(b *testing.B) {
	logger := log.NewLogger("bench")
	q, _ := NewQuasar(logger, 3, 2, 3)

	// Pre-populate with some finality records
	for i := 0; i < 1000; i++ {
		blockID := ids.GenerateTestID()
		q.finalized[blockID] = &QuantumFinality{
			BlockID:      blockID,
			PChainHeight: uint64(i),
		}
	}

	lookupID := ids.GenerateTestID()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = q.GetFinality(lookupID)
	}
}

// BenchmarkQuantumSigner_Sign benchmarks Ringtail signing
func BenchmarkQuantumSigner_Sign(b *testing.B) {
	logger := log.NewLogger("bench")
	signer := quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 100)

	key, _ := signer.GenerateRingtailKey()
	msg := make([]byte, 48)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = signer.Sign(msg, key)
	}
}

// BenchmarkQuantumSigner_Verify benchmarks Ringtail verification
func BenchmarkQuantumSigner_Verify(b *testing.B) {
	logger := log.NewLogger("bench")
	signer := quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 100)

	key, _ := signer.GenerateRingtailKey()
	msg := make([]byte, 48)
	sig, _ := signer.Sign(msg, key)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = signer.Verify(msg, sig)
	}
}

// BenchmarkQuantumSigner_KeyGen benchmarks Ringtail key generation
func BenchmarkQuantumSigner_KeyGen(b *testing.B) {
	logger := log.NewLogger("bench")
	signer := quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = signer.GenerateRingtailKey()
	}
}

// BenchmarkHybrid_AddValidator benchmarks validator addition
func BenchmarkHybrid_AddValidator(b *testing.B) {
	hybrid, _ := quasar.NewHybrid(3)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hybrid.AddValidator(fmt.Sprintf("validator-%d", i), 100)
	}
}

// BenchmarkHybrid_SignMessage benchmarks hybrid signing (BLS + Ringtail)
func BenchmarkHybrid_SignMessage(b *testing.B) {
	hybrid, _ := quasar.NewHybrid(3)

	validatorID := ids.GenerateTestNodeID().String()
	_ = hybrid.AddValidator(validatorID, 100)

	msg := make([]byte, 48)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = hybrid.SignMessage(validatorID, msg)
	}
}

// =============================================================================
// BLS vs HYBRID COMPARISON BENCHMARKS
// =============================================================================
// These benchmarks compare pure BLS performance vs hybrid (BLS + Ringtail)
// to measure the overhead of post-quantum security.
//
// Run with: go test -bench=Compare -benchmem ./vms/quantumvm/...

// BenchmarkCompare_PureBLS_Sign benchmarks pure BLS signing only
func BenchmarkCompare_PureBLS_Sign(b *testing.B) {
	sk, _ := bls.NewSecretKey()
	msg := make([]byte, 48)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sk.Sign(msg)
	}
}

// BenchmarkCompare_PureBLS_Verify benchmarks pure BLS verification only
func BenchmarkCompare_PureBLS_Verify(b *testing.B) {
	sk, _ := bls.NewSecretKey()
	pk := sk.PublicKey()
	msg := make([]byte, 48)
	sig, _ := sk.Sign(msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bls.Verify(pk, sig, msg)
	}
}

// BenchmarkCompare_PureBLS_Aggregate benchmarks BLS signature aggregation
func BenchmarkCompare_PureBLS_Aggregate(b *testing.B) {
	numSigs := 10
	sigs := make([]*bls.Signature, numSigs)
	msg := make([]byte, 48)

	for i := 0; i < numSigs; i++ {
		sk, _ := bls.NewSecretKey()
		sigs[i], _ = sk.Sign(msg)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = bls.AggregateSignatures(sigs)
	}
}

// BenchmarkCompare_Hybrid_Sign benchmarks hybrid (BLS + Ringtail) signing
func BenchmarkCompare_Hybrid_Sign(b *testing.B) {
	hybrid, _ := quasar.NewHybrid(3)
	validatorID := ids.GenerateTestNodeID().String()
	_ = hybrid.AddValidator(validatorID, 100)
	msg := make([]byte, 48)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = hybrid.SignMessage(validatorID, msg)
	}
}

// BenchmarkCompare_Hybrid_Verify benchmarks hybrid signature verification
func BenchmarkCompare_Hybrid_Verify(b *testing.B) {
	hybrid, _ := quasar.NewHybrid(3)
	validatorID := ids.GenerateTestNodeID().String()
	_ = hybrid.AddValidator(validatorID, 100)
	msg := make([]byte, 48)
	sig, _ := hybrid.SignMessage(validatorID, msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hybrid.VerifyHybridSignature(msg, sig)
	}
}

// BenchmarkCompare_Hybrid_Aggregate benchmarks hybrid signature aggregation
func BenchmarkCompare_Hybrid_Aggregate(b *testing.B) {
	hybrid, _ := quasar.NewHybrid(3)
	numSigs := 10
	sigs := make([]*quasar.HybridSignature, numSigs)
	msg := make([]byte, 48)

	for i := 0; i < numSigs; i++ {
		validatorID := fmt.Sprintf("validator-%d", i)
		_ = hybrid.AddValidator(validatorID, 100)
		sigs[i], _ = hybrid.SignMessage(validatorID, msg)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = hybrid.AggregateSignatures(msg, sigs)
	}
}

// BenchmarkCompare_Ringtail_Sign benchmarks pure Ringtail (post-quantum) signing
func BenchmarkCompare_Ringtail_Sign(b *testing.B) {
	logger := log.NewLogger("bench")
	signer := quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 100)
	key, _ := signer.GenerateRingtailKey()
	msg := make([]byte, 48)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = signer.Sign(msg, key)
	}
}

// BenchmarkCompare_Ringtail_Verify benchmarks pure Ringtail verification
func BenchmarkCompare_Ringtail_Verify(b *testing.B) {
	logger := log.NewLogger("bench")
	signer := quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 100)
	key, _ := signer.GenerateRingtailKey()
	msg := make([]byte, 48)
	sig, _ := signer.Sign(msg, key)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = signer.Verify(msg, sig)
	}
}

// TestCompare_BLSvsHybrid_Detailed runs detailed comparison and prints results
func TestCompare_BLSvsHybrid_Detailed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping detailed comparison in short mode")
	}

	require := require.New(t)
	numIterations := 1000

	// Setup BLS
	blsSK, err := bls.NewSecretKey()
	require.NoError(err)
	blsPK := blsSK.PublicKey()
	msg := make([]byte, 48)
	copy(msg, "test message for signing benchmark")

	// Setup Hybrid
	hybrid, err := quasar.NewHybrid(3)
	require.NoError(err)
	validatorID := ids.GenerateTestNodeID().String()
	err = hybrid.AddValidator(validatorID, 100)
	require.NoError(err)

	// Setup Ringtail
	logger := log.NewLogger("bench")
	ringtailSigner := quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 100)
	ringtailKey, err := ringtailSigner.GenerateRingtailKey()
	require.NoError(err)

	t.Log("═══════════════════════════════════════════════════════════════════")
	t.Log("               BLS vs HYBRID vs RINGTAIL COMPARISON")
	t.Log("═══════════════════════════════════════════════════════════════════")
	t.Logf("Iterations: %d", numIterations)
	t.Log("")

	// ========================
	// SIGNING COMPARISON
	// ========================
	t.Log("SIGNING PERFORMANCE:")
	t.Log("───────────────────────────────────────────────────────────────────")

	// Pure BLS signing
	blsSignStart := time.Now()
	var blsSig *bls.Signature
	for i := 0; i < numIterations; i++ {
		blsSig, _ = blsSK.Sign(msg)
	}
	blsSignTime := time.Since(blsSignStart)
	blsSignPerOp := blsSignTime / time.Duration(numIterations)

	// Hybrid signing (BLS + Ringtail)
	hybridSignStart := time.Now()
	var hybridSig *quasar.HybridSignature
	for i := 0; i < numIterations; i++ {
		hybridSig, _ = hybrid.SignMessage(validatorID, msg)
	}
	hybridSignTime := time.Since(hybridSignStart)
	hybridSignPerOp := hybridSignTime / time.Duration(numIterations)

	// Pure Ringtail signing
	ringtailSignStart := time.Now()
	var ringtailSig *quantum.QuantumSignature
	for i := 0; i < numIterations; i++ {
		ringtailSig, _ = ringtailSigner.Sign(msg, ringtailKey)
	}
	ringtailSignTime := time.Since(ringtailSignStart)
	ringtailSignPerOp := ringtailSignTime / time.Duration(numIterations)

	signOverhead := float64(hybridSignPerOp) / float64(blsSignPerOp)

	t.Logf("  Pure BLS:      %12v/op  (baseline)", blsSignPerOp)
	t.Logf("  Pure Ringtail: %12v/op  (%.2fx BLS)", ringtailSignPerOp, float64(ringtailSignPerOp)/float64(blsSignPerOp))
	t.Logf("  Hybrid:        %12v/op  (%.2fx BLS = BLS + Ringtail overhead)", hybridSignPerOp, signOverhead)
	t.Log("")

	// ========================
	// VERIFICATION COMPARISON
	// ========================
	t.Log("VERIFICATION PERFORMANCE:")
	t.Log("───────────────────────────────────────────────────────────────────")

	// Pure BLS verification
	blsVerifyStart := time.Now()
	for i := 0; i < numIterations; i++ {
		_ = bls.Verify(blsPK, blsSig, msg)
	}
	blsVerifyTime := time.Since(blsVerifyStart)
	blsVerifyPerOp := blsVerifyTime / time.Duration(numIterations)

	// Hybrid verification
	hybridVerifyStart := time.Now()
	for i := 0; i < numIterations; i++ {
		_ = hybrid.VerifyHybridSignature(msg, hybridSig)
	}
	hybridVerifyTime := time.Since(hybridVerifyStart)
	hybridVerifyPerOp := hybridVerifyTime / time.Duration(numIterations)

	// Pure Ringtail verification
	ringtailVerifyStart := time.Now()
	for i := 0; i < numIterations; i++ {
		_ = ringtailSigner.Verify(msg, ringtailSig)
	}
	ringtailVerifyTime := time.Since(ringtailVerifyStart)
	ringtailVerifyPerOp := ringtailVerifyTime / time.Duration(numIterations)

	verifyOverhead := float64(hybridVerifyPerOp) / float64(blsVerifyPerOp)

	t.Logf("  Pure BLS:      %12v/op  (baseline)", blsVerifyPerOp)
	t.Logf("  Pure Ringtail: %12v/op  (%.2fx BLS)", ringtailVerifyPerOp, float64(ringtailVerifyPerOp)/float64(blsVerifyPerOp))
	t.Logf("  Hybrid:        %12v/op  (%.2fx BLS)", hybridVerifyPerOp, verifyOverhead)
	t.Log("")

	// ========================
	// AGGREGATION COMPARISON
	// ========================
	t.Log("AGGREGATION PERFORMANCE (10 signatures):")
	t.Log("───────────────────────────────────────────────────────────────────")

	// Setup multiple BLS signatures
	numSigs := 10
	blsSigs := make([]*bls.Signature, numSigs)
	for i := 0; i < numSigs; i++ {
		sk, _ := bls.NewSecretKey()
		blsSigs[i], _ = sk.Sign(msg)
	}

	// Setup multiple hybrid signatures
	hybridSigs := make([]*quasar.HybridSignature, numSigs)
	for i := 0; i < numSigs; i++ {
		vID := fmt.Sprintf("agg-validator-%d", i)
		_ = hybrid.AddValidator(vID, 100)
		hybridSigs[i], _ = hybrid.SignMessage(vID, msg)
	}

	// BLS aggregation
	blsAggStart := time.Now()
	for i := 0; i < numIterations; i++ {
		_, _ = bls.AggregateSignatures(blsSigs)
	}
	blsAggTime := time.Since(blsAggStart)
	blsAggPerOp := blsAggTime / time.Duration(numIterations)

	// Hybrid aggregation
	hybridAggStart := time.Now()
	for i := 0; i < numIterations; i++ {
		_, _ = hybrid.AggregateSignatures(msg, hybridSigs)
	}
	hybridAggTime := time.Since(hybridAggStart)
	hybridAggPerOp := hybridAggTime / time.Duration(numIterations)

	aggOverhead := float64(hybridAggPerOp) / float64(blsAggPerOp)

	t.Logf("  Pure BLS:      %12v/op  (baseline)", blsAggPerOp)
	t.Logf("  Hybrid:        %12v/op  (%.2fx BLS)", hybridAggPerOp, aggOverhead)
	t.Log("")

	// ========================
	// SIGNATURE SIZES
	// ========================
	t.Log("SIGNATURE SIZES:")
	t.Log("───────────────────────────────────────────────────────────────────")

	blsSigBytes := bls.SignatureToBytes(blsSig)
	t.Logf("  Pure BLS:      %6d bytes", len(blsSigBytes))
	t.Logf("  Ringtail:      %6d bytes", len(ringtailSig.Signature))
	t.Logf("  Hybrid BLS:    %6d bytes", len(hybridSig.BLS))
	t.Logf("  Hybrid RT:     %6d bytes", len(hybridSig.Ringtail))
	t.Logf("  Hybrid Total:  %6d bytes (%.2fx BLS)", len(hybridSig.BLS)+len(hybridSig.Ringtail),
		float64(len(hybridSig.BLS)+len(hybridSig.Ringtail))/float64(len(blsSigBytes)))
	t.Log("")

	// ========================
	// SUMMARY
	// ========================
	t.Log("═══════════════════════════════════════════════════════════════════")
	t.Log("SUMMARY - HYBRID OVERHEAD FOR POST-QUANTUM SECURITY:")
	t.Log("═══════════════════════════════════════════════════════════════════")
	t.Logf("  Signing overhead:      %.2fx slower than pure BLS", signOverhead)
	t.Logf("  Verification overhead: %.2fx slower than pure BLS", verifyOverhead)
	t.Logf("  Aggregation overhead:  %.2fx slower than pure BLS", aggOverhead)
	t.Logf("  Signature size:        %.2fx larger than pure BLS",
		float64(len(hybridSig.BLS)+len(hybridSig.Ringtail))/float64(len(blsSigBytes)))
	t.Log("")
	t.Log("This overhead provides protection against quantum computer attacks.")
	t.Log("═══════════════════════════════════════════════════════════════════")
}

// BenchmarkParallel_Stats benchmarks concurrent stats reads
func BenchmarkParallel_Stats(b *testing.B) {
	logger := log.NewLogger("bench")
	q, _ := NewQuasar(logger, 3, 2, 3)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = q.Stats()
		}
	})
}

// BenchmarkParallel_MessageCreation benchmarks concurrent message creation
func BenchmarkParallel_MessageCreation(b *testing.B) {
	logger := log.NewLogger("bench")
	q, _ := NewQuasar(logger, 3, 2, 3)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			event := FinalityEvent{
				Height:    12345,
				BlockID:   ids.GenerateTestID(),
				Timestamp: time.Now(),
			}
			_ = q.createMessage(event)
		}
	})
}

// =============================================================================
// PERFORMANCE TESTS (measuring actual throughput)
// =============================================================================

// TestPerformance_FinalityThroughput measures finality events per second
func TestPerformance_FinalityThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	require := require.New(t)

	logger := log.NewLogger("perf-test")
	q, err := NewQuasar(logger, 3, 2, 3)
	require.NoError(err)

	pChain := newMockPChain(10)
	q.ConnectPChain(pChain)

	vm := &VM{
		quantumSigner: quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 1000),
	}
	q.ConnectQChain(vm)

	ctx := context.Background()
	err = q.Start(ctx)
	require.NoError(err)

	finalityCh := q.Subscribe()

	// Emit many blocks
	numBlocks := 100
	start := time.Now()

	go func() {
		for i := 0; i < numBlocks; i++ {
			pChain.emitFinality(ids.GenerateTestID())
		}
	}()

	// Count finalized blocks
	finalized := 0
	timeout := time.After(30 * time.Second)

	for finalized < numBlocks {
		select {
		case <-finalityCh:
			finalized++
		case <-timeout:
			t.Fatalf("timeout: only finalized %d/%d blocks", finalized, numBlocks)
		}
	}

	elapsed := time.Since(start)
	throughput := float64(numBlocks) / elapsed.Seconds()

	q.Stop()

	t.Logf("═══════════════════════════════════════════")
	t.Logf("FINALITY THROUGHPUT: %.2f blocks/sec", throughput)
	t.Logf("Total blocks: %d", numBlocks)
	t.Logf("Total time: %v", elapsed)
	t.Logf("Avg latency: %v per block", elapsed/time.Duration(numBlocks))
	t.Logf("═══════════════════════════════════════════")
}

// TestPerformance_PQSyncLatency measures P-Chain to Q-Chain sync latency
func TestPerformance_PQSyncLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	require := require.New(t)

	logger := log.NewLogger("latency-test")
	q, err := NewQuasar(logger, 3, 2, 3)
	require.NoError(err)

	pChain := newMockPChain(10)
	q.ConnectPChain(pChain)

	vm := &VM{
		quantumSigner: quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 100),
	}
	q.ConnectQChain(vm)

	ctx := context.Background()
	err = q.Start(ctx)
	require.NoError(err)

	finalityCh := q.Subscribe()

	// Measure latencies
	numSamples := 50
	latencies := make([]time.Duration, 0, numSamples)

	for i := 0; i < numSamples; i++ {
		blockID := ids.GenerateTestID()
		start := time.Now()

		pChain.emitFinality(blockID)

		select {
		case finality := <-finalityCh:
			latency := time.Since(start)
			latencies = append(latencies, latency)
			require.Equal(blockID, finality.BlockID)
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for block %d", i)
		}
	}

	q.Stop()

	// Calculate statistics
	var total time.Duration
	var min, max time.Duration = latencies[0], latencies[0]

	for _, l := range latencies {
		total += l
		if l < min {
			min = l
		}
		if l > max {
			max = l
		}
	}
	avg := total / time.Duration(len(latencies))

	t.Logf("═══════════════════════════════════════════")
	t.Logf("P/Q SYNC LATENCY STATISTICS")
	t.Logf("Samples: %d", numSamples)
	t.Logf("Min:     %v", min)
	t.Logf("Max:     %v", max)
	t.Logf("Avg:     %v", avg)
	t.Logf("═══════════════════════════════════════════")
}

// TestPerformance_ConcurrentValidators tests performance with many validators
func TestPerformance_ConcurrentValidators(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	require := require.New(t)

	validatorCounts := []int{10, 50, 100, 200}

	for _, numValidators := range validatorCounts {
		t.Run(fmt.Sprintf("%d_validators", numValidators), func(t *testing.T) {
			logger := log.NewLogger("validator-perf")
			q, err := NewQuasar(logger, numValidators/2, 2, 3)
			require.NoError(err)

			pChain := newMockPChain(numValidators)
			q.ConnectPChain(pChain)

			vm := &VM{
				quantumSigner: quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 100),
			}
			q.ConnectQChain(vm)

			ctx := context.Background()
			err = q.Start(ctx)
			require.NoError(err)

			finalityCh := q.Subscribe()

			// Time single finality event
			numBlocks := 20
			start := time.Now()

			go func() {
				for i := 0; i < numBlocks; i++ {
					pChain.emitFinality(ids.GenerateTestID())
				}
			}()

			for i := 0; i < numBlocks; i++ {
				select {
				case <-finalityCh:
				case <-time.After(10 * time.Second):
					t.Fatalf("timeout at block %d", i)
				}
			}

			elapsed := time.Since(start)
			q.Stop()

			t.Logf("%d validators: %d blocks in %v (%.2f blocks/sec)",
				numValidators, numBlocks, elapsed,
				float64(numBlocks)/elapsed.Seconds())
		})
	}
}

// TestPerformance_QuantumSignatureOverhead measures quantum signature overhead
func TestPerformance_QuantumSignatureOverhead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	require := require.New(t)

	logger := log.NewLogger("overhead-test")
	signer := quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 100)

	key, err := signer.GenerateRingtailKey()
	require.NoError(err)

	msg := make([]byte, 48)
	numOps := 1000

	// Measure signing
	signStart := time.Now()
	var sig *quantum.QuantumSignature
	for i := 0; i < numOps; i++ {
		sig, _ = signer.Sign(msg, key)
	}
	signElapsed := time.Since(signStart)

	// Measure verification
	verifyStart := time.Now()
	for i := 0; i < numOps; i++ {
		_ = signer.Verify(msg, sig)
	}
	verifyElapsed := time.Since(verifyStart)

	t.Logf("═══════════════════════════════════════════")
	t.Logf("QUANTUM SIGNATURE OVERHEAD")
	t.Logf("Operations: %d", numOps)
	t.Logf("Signing:    %v total, %v/op", signElapsed, signElapsed/time.Duration(numOps))
	t.Logf("Verify:     %v total, %v/op", verifyElapsed, verifyElapsed/time.Duration(numOps))
	t.Logf("Sig size:   %d bytes", len(sig.Signature))
	t.Logf("═══════════════════════════════════════════")
}

// TestPerformance_ParallelVerification tests parallel signature verification
func TestPerformance_ParallelVerification(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	require := require.New(t)

	logger := log.NewLogger("parallel-verify-test")
	signer := quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 1000)

	// Generate messages and signatures
	numSigs := 100
	messages := make([][]byte, numSigs)
	signatures := make([]*quantum.QuantumSignature, numSigs)

	for i := 0; i < numSigs; i++ {
		key, _ := signer.GenerateRingtailKey()
		messages[i] = make([]byte, 48)
		messages[i][0] = byte(i)
		signatures[i], _ = signer.Sign(messages[i], key)
	}

	// Sequential verification
	seqStart := time.Now()
	for i := 0; i < numSigs; i++ {
		_ = signer.Verify(messages[i], signatures[i])
	}
	seqElapsed := time.Since(seqStart)

	// Parallel verification
	parStart := time.Now()
	err := signer.ParallelVerify(messages, signatures)
	require.NoError(err)
	parElapsed := time.Since(parStart)

	speedup := float64(seqElapsed) / float64(parElapsed)

	t.Logf("═══════════════════════════════════════════")
	t.Logf("PARALLEL VERIFICATION PERFORMANCE")
	t.Logf("Signatures: %d", numSigs)
	t.Logf("Sequential: %v", seqElapsed)
	t.Logf("Parallel:   %v", parElapsed)
	t.Logf("Speedup:    %.2fx", speedup)
	t.Logf("═══════════════════════════════════════════")
}

// =============================================================================
// STRESS TESTS
// =============================================================================

// TestStress_HighLoad tests system under high load
func TestStress_HighLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	require := require.New(t)

	logger := log.NewLogger("stress-test")
	q, err := NewQuasar(logger, 5, 2, 3)
	require.NoError(err)

	pChain := newMockPChain(20)
	q.ConnectPChain(pChain)

	vm := &VM{
		quantumSigner: quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 1000),
	}
	q.ConnectQChain(vm)

	ctx := context.Background()
	err = q.Start(ctx)
	require.NoError(err)

	finalityCh := q.Subscribe()

	// High frequency finality events
	numBlocks := 500
	var emitted, finalized int64

	// Producer: emit blocks rapidly
	go func() {
		for i := 0; i < numBlocks; i++ {
			pChain.emitFinality(ids.GenerateTestID())
			atomic.AddInt64(&emitted, 1)
		}
	}()

	// Consumer: count finalized
	timeout := time.After(60 * time.Second)
	for atomic.LoadInt64(&finalized) < int64(numBlocks) {
		select {
		case <-finalityCh:
			atomic.AddInt64(&finalized, 1)
		case <-timeout:
			t.Fatalf("stress test timeout: emitted=%d, finalized=%d",
				atomic.LoadInt64(&emitted), atomic.LoadInt64(&finalized))
		}
	}

	q.Stop()

	t.Logf("✓ Stress test passed: %d blocks finalized under high load", numBlocks)
}

// TestStress_BurstTraffic tests handling of burst traffic
func TestStress_BurstTraffic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	require := require.New(t)

	logger := log.NewLogger("burst-test")
	q, err := NewQuasar(logger, 3, 2, 3)
	require.NoError(err)

	pChain := newMockPChain(10)
	q.ConnectPChain(pChain)

	vm := &VM{
		quantumSigner: quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 1000),
	}
	q.ConnectQChain(vm)

	ctx := context.Background()
	err = q.Start(ctx)
	require.NoError(err)

	finalityCh := q.Subscribe()

	// Burst pattern: 50 blocks, pause, 50 blocks, pause...
	burstSize := 50
	numBursts := 5
	totalBlocks := burstSize * numBursts

	var finalized int64

	// Consumer
	go func() {
		for atomic.LoadInt64(&finalized) < int64(totalBlocks) {
			select {
			case <-finalityCh:
				atomic.AddInt64(&finalized, 1)
			case <-time.After(30 * time.Second):
				return
			}
		}
	}()

	// Producer with bursts
	for burst := 0; burst < numBursts; burst++ {
		// Emit burst
		for i := 0; i < burstSize; i++ {
			pChain.emitFinality(ids.GenerateTestID())
		}
		// Pause between bursts
		time.Sleep(100 * time.Millisecond)
	}

	// Wait for all to finalize
	timeout := time.After(60 * time.Second)
	for atomic.LoadInt64(&finalized) < int64(totalBlocks) {
		select {
		case <-time.After(100 * time.Millisecond):
		case <-timeout:
			t.Fatalf("burst test timeout: finalized %d/%d",
				atomic.LoadInt64(&finalized), totalBlocks)
		}
	}

	q.Stop()

	t.Logf("✓ Burst test passed: %d blocks in %d bursts", totalBlocks, numBursts)
}

// TestStress_LongRunning tests stability over extended operation
func TestStress_LongRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long-running test in short mode")
	}

	require := require.New(t)

	logger := log.NewLogger("long-running-test")
	q, err := NewQuasar(logger, 3, 2, 3)
	require.NoError(err)

	pChain := newMockPChain(10)
	q.ConnectPChain(pChain)

	vm := &VM{
		quantumSigner: quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 1000),
	}
	q.ConnectQChain(vm)

	ctx := context.Background()
	err = q.Start(ctx)
	require.NoError(err)

	finalityCh := q.Subscribe()

	// Run for 10 seconds with steady block production
	duration := 10 * time.Second
	blockInterval := 50 * time.Millisecond

	var emitted, finalized int64
	done := make(chan struct{})

	// Producer
	go func() {
		ticker := time.NewTicker(blockInterval)
		defer ticker.Stop()

		timer := time.After(duration)
		for {
			select {
			case <-ticker.C:
				pChain.emitFinality(ids.GenerateTestID())
				atomic.AddInt64(&emitted, 1)
			case <-timer:
				close(done)
				return
			}
		}
	}()

	// Consumer
	go func() {
		for {
			select {
			case <-finalityCh:
				atomic.AddInt64(&finalized, 1)
			case <-done:
				return
			}
		}
	}()

	<-done
	// Wait a bit for remaining finality events
	time.Sleep(500 * time.Millisecond)

	q.Stop()

	e := atomic.LoadInt64(&emitted)
	f := atomic.LoadInt64(&finalized)
	rate := float64(f) / duration.Seconds()

	t.Logf("═══════════════════════════════════════════")
	t.Logf("LONG-RUNNING TEST RESULTS")
	t.Logf("Duration:   %v", duration)
	t.Logf("Emitted:    %d blocks", e)
	t.Logf("Finalized:  %d blocks", f)
	t.Logf("Rate:       %.2f blocks/sec", rate)
	t.Logf("Drop rate:  %.2f%%", 100*(1-float64(f)/float64(e)))
	t.Logf("═══════════════════════════════════════════")
}

// =============================================================================
// MEMORY TESTS
// =============================================================================

// TestMemory_FinalityMapGrowth tests memory usage of finality map
func TestMemory_FinalityMapGrowth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory test in short mode")
	}

	logger := log.NewLogger("memory-test")
	q, _ := NewQuasar(logger, 3, 2, 3)

	// Simulate many finality records
	numRecords := 10000

	for i := 0; i < numRecords; i++ {
		blockID := ids.GenerateTestID()
		q.finalized[blockID] = &QuantumFinality{
			BlockID:       blockID,
			PChainHeight:  uint64(i),
			QChainHeight:  uint64(i),
			BLSProof:      make([]byte, 96),
			RingtailProof: make([]byte, 128),
			SignerBitset:  make([]byte, 32),
			TotalWeight:   1000,
			SignerWeight:  800,
			Timestamp:     time.Now(),
		}
	}

	stats := q.Stats()
	t.Logf("Finality map holds %d records", stats.FinalizedBlocks)
	t.Logf("✓ Memory test completed - consider implementing pruning for production")
}

// =============================================================================
// CONCURRENT BENCHMARK
// =============================================================================

// BenchmarkConcurrent_MixedOperations benchmarks realistic mixed workload
func BenchmarkConcurrent_MixedOperations(b *testing.B) {
	logger := log.NewLogger("bench")
	q, _ := NewQuasar(logger, 3, 2, 3)

	pChain := newMockPChain(10)
	q.ConnectPChain(pChain)

	vm := &VM{
		quantumSigner: quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 100),
	}
	q.ConnectQChain(vm)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			switch i % 5 {
			case 0:
				_ = q.Stats()
			case 1:
				_, _ = q.GetFinality(ids.GenerateTestID())
			case 2:
				_ = q.checkQuorum(67, 100)
			case 3:
				_ = q.totalWeight(pChain.validators)
			case 4:
				event := FinalityEvent{
					Height:    uint64(i),
					BlockID:   ids.GenerateTestID(),
					Timestamp: time.Now(),
				}
				_ = q.createMessage(event)
			}
			i++
		}
	})
}

// =============================================================================
// SYNCHRONIZATION BENCHMARK
// =============================================================================

// BenchmarkPQSync_EndToEnd benchmarks full P->Q sync cycle
func BenchmarkPQSync_EndToEnd(b *testing.B) {
	logger := log.NewLogger("bench")

	for i := 0; i < b.N; i++ {
		b.StopTimer()

		q, _ := NewQuasar(logger, 3, 2, 3)
		pChain := newMockPChain(5)
		q.ConnectPChain(pChain)

		vm := &VM{
			quantumSigner: quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 100),
		}
		q.ConnectQChain(vm)

		ctx := context.Background()
		_ = q.Start(ctx)
		finalityCh := q.Subscribe()

		b.StartTimer()

		// Measure single finality cycle
		pChain.emitFinality(ids.GenerateTestID())
		<-finalityCh

		b.StopTimer()
		q.Stop()
	}
}

// =============================================================================
// LOCK CONTENTION TESTS
// =============================================================================

// TestLockContention_ReadHeavy tests performance with read-heavy workload
func TestLockContention_ReadHeavy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contention test in short mode")
	}

	logger := log.NewLogger("contention-test")
	q, _ := NewQuasar(logger, 3, 2, 3)

	// Pre-populate
	for i := 0; i < 1000; i++ {
		blockID := ids.GenerateTestID()
		q.finalized[blockID] = &QuantumFinality{BlockID: blockID}
	}

	var wg sync.WaitGroup
	numReaders := 100
	numOpsPerReader := 10000
	var totalOps int64

	start := time.Now()

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOpsPerReader; j++ {
				_ = q.Stats()
				atomic.AddInt64(&totalOps, 1)
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	opsPerSec := float64(totalOps) / elapsed.Seconds()
	t.Logf("Read-heavy: %d ops in %v (%.0f ops/sec)",
		totalOps, elapsed, opsPerSec)
}

// TestLockContention_WriteHeavy tests performance with write-heavy workload
func TestLockContention_WriteHeavy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contention test in short mode")
	}

	logger := log.NewLogger("contention-test")
	q, _ := NewQuasar(logger, 3, 2, 3)

	var wg sync.WaitGroup
	numWriters := 50
	numOpsPerWriter := 1000
	var totalOps int64

	start := time.Now()

	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOpsPerWriter; j++ {
				blockID := ids.GenerateTestID()
				q.mu.Lock()
				q.finalized[blockID] = &QuantumFinality{BlockID: blockID}
				q.mu.Unlock()
				atomic.AddInt64(&totalOps, 1)
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	opsPerSec := float64(totalOps) / elapsed.Seconds()
	t.Logf("Write-heavy: %d ops in %v (%.0f ops/sec)",
		totalOps, elapsed, opsPerSec)
}
