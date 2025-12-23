// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tvm

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/threshold/pkg/math/curve"
	"github.com/luxfi/threshold/pkg/party"
	"github.com/luxfi/threshold/pkg/pool"
	"github.com/luxfi/threshold/pkg/protocol"
	"github.com/luxfi/threshold/protocols/bls"
	"github.com/luxfi/threshold/protocols/cmp"
	"github.com/luxfi/threshold/protocols/frost"
	"github.com/luxfi/threshold/protocols/lss"
	"github.com/luxfi/threshold/protocols/ringtail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Harness - Local implementation for MPC protocol testing
// =============================================================================

// testNetwork simulates a network for MPC protocol testing
type testNetwork struct {
	parties  map[party.ID]chan *protocol.Message
	handlers map[party.ID]*protocol.Handler
	mu       sync.RWMutex
	wg       sync.WaitGroup
	done     chan struct{}
}

func newTestNetwork(ids []party.ID) *testNetwork {
	n := &testNetwork{
		parties:  make(map[party.ID]chan *protocol.Message),
		handlers: make(map[party.ID]*protocol.Handler),
		done:     make(chan struct{}),
	}
	for _, id := range ids {
		n.parties[id] = make(chan *protocol.Message, 10000)
	}
	return n
}

func (n *testNetwork) close() {
	close(n.done)
	n.wg.Wait()
	for _, ch := range n.parties {
		close(ch)
	}
}

// runTestProtocol runs a protocol to completion across all parties
func runTestProtocol(
	t testing.TB,
	ids []party.ID,
	createStart func(id party.ID) protocol.StartFunc,
) (map[party.ID]interface{}, error) {
	network := newTestNetwork(ids)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	defer network.close()

	logger := log.NewNoOpLogger()
	results := sync.Map{}
	errors := sync.Map{}

	// Create handlers for all parties
	for _, id := range ids {
		startFunc := createStart(id)
		handler, err := protocol.NewHandler(
			ctx,
			logger,
			nil,
			startFunc,
			[]byte("test-session"),
			protocol.DefaultConfig(),
		)
		if err != nil {
			return nil, err
		}
		network.handlers[id] = handler
	}

	// Start message routing for each party
	for _, partyID := range ids {
		id := partyID
		handler := network.handlers[id]

		// Outgoing messages
		network.wg.Add(1)
		go func() {
			defer network.wg.Done()
			for {
				select {
				case <-network.done:
					return
				case msg := <-handler.Listen():
					if msg == nil {
						return
					}
					// Route message
					network.mu.RLock()
					if msg.To == "" {
						// Broadcast
						for toID, ch := range network.parties {
							if toID != id {
								select {
								case ch <- msg:
								default:
								}
							}
						}
					} else {
						// Point-to-point
						if ch, ok := network.parties[party.ID(msg.To)]; ok {
							select {
							case ch <- msg:
							default:
							}
						}
					}
					network.mu.RUnlock()
				}
			}
		}()

		// Incoming messages
		network.wg.Add(1)
		go func() {
			defer network.wg.Done()
			ch := network.parties[id]
			for {
				select {
				case <-network.done:
					return
				case msg := <-ch:
					if msg != nil {
						handler.Accept(msg)
					}
				}
			}
		}()
	}

	// Wait for all parties to complete
	var wg sync.WaitGroup
	for id, handler := range network.handlers {
		id := id
		handler := handler
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := handler.WaitForResult()
			if err != nil {
				errors.Store(id, err)
			} else {
				results.Store(id, result)
			}
		}()
	}

	wg.Wait()

	// Check for errors
	var errs []error
	errors.Range(func(key, value interface{}) bool {
		errs = append(errs, value.(error))
		return true
	})
	if len(errs) > 0 {
		return nil, errs[0]
	}

	// Collect results
	resultMap := make(map[party.ID]interface{})
	results.Range(func(key, value interface{}) bool {
		resultMap[key.(party.ID)] = value
		return true
	})

	return resultMap, nil
}

// testPartyIDs generates party IDs for testing
func testPartyIDs(n int) []party.ID {
	ids := make([]party.ID, n)
	for i := 0; i < n; i++ {
		ids[i] = party.ID(string(rune('a' + i)))
	}
	return ids
}

// =============================================================================
// Unit Tests
// =============================================================================

// TestProtocolExecutorCreation tests creating a ProtocolExecutor
func TestProtocolExecutorCreation(t *testing.T) {
	require := require.New(t)

	workerPool := pool.NewPool(4)
	defer workerPool.TearDown()
	logger := log.NewNoOpLogger()

	pe := NewProtocolExecutor(workerPool, logger)
	require.NotNil(pe)
	require.NotNil(pe.pool)
	require.NotNil(pe.handlers)
}

// TestLSSKeygenStartFunc tests LSS key generation start function
func TestLSSKeygenStartFunc(t *testing.T) {
	require := require.New(t)

	workerPool := pool.NewPool(4)
	defer workerPool.TearDown()
	logger := log.NewNoOpLogger()
	pe := NewProtocolExecutor(workerPool, logger)

	pIDs := []party.ID{"alice", "bob", "charlie"}
	selfID := party.ID("alice")
	threshold := 2

	startFunc := pe.LSSKeygenStartFunc(selfID, pIDs, threshold)
	require.NotNil(startFunc)
}

// TestCMPKeygenStartFunc tests CMP key generation start function
func TestCMPKeygenStartFunc(t *testing.T) {
	require := require.New(t)

	workerPool := pool.NewPool(4)
	defer workerPool.TearDown()
	logger := log.NewNoOpLogger()
	pe := NewProtocolExecutor(workerPool, logger)

	pIDs := []party.ID{"alice", "bob", "charlie"}
	selfID := party.ID("alice")
	threshold := 2

	startFunc := pe.CMPKeygenStartFunc(selfID, pIDs, threshold)
	require.NotNil(startFunc)
}

// TestFROSTKeygenStartFunc tests FROST key generation start function
func TestFROSTKeygenStartFunc(t *testing.T) {
	require := require.New(t)

	workerPool := pool.NewPool(4)
	defer workerPool.TearDown()
	logger := log.NewNoOpLogger()
	pe := NewProtocolExecutor(workerPool, logger)

	pIDs := []party.ID{"alice", "bob", "charlie"}
	selfID := party.ID("alice")
	threshold := 2

	startFunc := pe.FROSTKeygenStartFunc(selfID, pIDs, threshold)
	require.NotNil(startFunc)
}

// TestFROSTKeygenTaprootStartFunc tests FROST Taproot key generation
func TestFROSTKeygenTaprootStartFunc(t *testing.T) {
	require := require.New(t)

	workerPool := pool.NewPool(4)
	defer workerPool.TearDown()
	logger := log.NewNoOpLogger()
	pe := NewProtocolExecutor(workerPool, logger)

	pIDs := []party.ID{"alice", "bob", "charlie"}
	selfID := party.ID("alice")
	threshold := 2

	startFunc := pe.FROSTKeygenTaprootStartFunc(selfID, pIDs, threshold)
	require.NotNil(startFunc)
}

// TestHandlerLifecycle tests creating, getting, and removing handlers
func TestHandlerLifecycle(t *testing.T) {
	require := require.New(t)

	workerPool := pool.NewPool(4)
	defer workerPool.TearDown()
	logger := log.NewNoOpLogger()
	pe := NewProtocolExecutor(workerPool, logger)

	pIDs := []party.ID{"alice", "bob", "charlie"}
	selfID := party.ID("alice")
	threshold := 2

	ctx := context.Background()
	sessionID := "test-session-1"
	startFunc := pe.FROSTKeygenStartFunc(selfID, pIDs, threshold)

	handler, err := pe.CreateHandler(ctx, sessionID, startFunc)
	require.NoError(err)
	require.NotNil(handler)

	retrieved, ok := pe.GetHandler(sessionID)
	require.True(ok)
	require.Equal(handler, retrieved)

	_, ok = pe.GetHandler("non-existent")
	require.False(ok)

	pe.RemoveHandler(sessionID)

	_, ok = pe.GetHandler(sessionID)
	require.False(ok)
}

// TestKeyShareWrappers tests the KeyShare wrapper implementations
func TestKeyShareWrappers(t *testing.T) {
	require := require.New(t)

	lssShare := &LSSKeyShare{Config: nil}
	require.Equal(ProtocolLSS, lssShare.Protocol())

	cmpShare := &CMPKeyShare{Config: nil}
	require.Equal(ProtocolCGGMP21, cmpShare.Protocol())

	frostShare := &FROSTKeyShare{Config: nil}
	require.Equal(ProtocolFrost, frostShare.Protocol())
}

// TestECDSASignatureType tests ECDSA signature type
func TestECDSASignatureType(t *testing.T) {
	require := require.New(t)

	sig := &ECDSASignature{
		R: make([]byte, 32),
		S: make([]byte, 32),
		V: 27,
	}

	require.Len(sig.R, 32)
	require.Len(sig.S, 32)
	require.Equal(byte(27), sig.V)
}

// TestSchnorrSignatureType tests Schnorr signature type
func TestSchnorrSignatureType(t *testing.T) {
	require := require.New(t)

	sig := &SchnorrSignature{
		R: make([]byte, 32),
		Z: make([]byte, 32),
	}

	require.Len(sig.R, 32)
	require.Len(sig.Z, 32)
}

// =============================================================================
// Full Protocol Execution Tests
// =============================================================================

// TestFROSTKeygenFullExecution runs a complete FROST keygen
func TestFROSTKeygenFullExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping FROST keygen execution in short mode")
	}

	require := require.New(t)

	pIDs := testPartyIDs(5)
	threshold := 3

	results, err := runTestProtocol(t, pIDs, func(id party.ID) protocol.StartFunc {
		return frost.Keygen(curve.Secp256k1{}, id, pIDs, threshold)
	})
	require.NoError(err)
	require.Len(results, 5)

	var firstPubKey curve.Point
	for id, result := range results {
		config, ok := result.(*frost.Config)
		require.True(ok, "result should be *frost.Config for party %s", id)
		require.NotNil(config)
		require.NotNil(config.PublicKey)

		if firstPubKey == nil {
			firstPubKey = config.PublicKey
		} else {
			assert.True(t, firstPubKey.Equal(config.PublicKey),
				"all parties should have same public key")
		}
	}

	t.Logf("FROST keygen completed: 5 parties, threshold=%d", threshold)
}

// TestLSSKeygenFullExecution runs a complete LSS keygen
func TestLSSKeygenFullExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LSS keygen execution in short mode")
	}

	require := require.New(t)

	workerPool := pool.NewPool(4)
	defer workerPool.TearDown()

	pIDs := testPartyIDs(3)
	threshold := 2

	results, err := runTestProtocol(t, pIDs, func(id party.ID) protocol.StartFunc {
		return lss.Keygen(curve.Secp256k1{}, id, pIDs, threshold, workerPool)
	})
	require.NoError(err)
	require.Len(results, 3)

	for id, result := range results {
		config, ok := result.(*lss.Config)
		require.True(ok, "result should be *lss.Config for party %s", id)
		require.NotNil(config)
	}

	t.Logf("LSS keygen completed: 3 parties, threshold=%d", threshold)
}

// TestCMPKeygenFullExecution runs a complete CMP keygen
func TestCMPKeygenFullExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CMP keygen execution in short mode")
	}

	require := require.New(t)

	workerPool := pool.NewPool(4)
	defer workerPool.TearDown()

	pIDs := testPartyIDs(3)
	threshold := 2

	results, err := runTestProtocol(t, pIDs, func(id party.ID) protocol.StartFunc {
		return cmp.Keygen(curve.Secp256k1{}, id, pIDs, threshold, workerPool)
	})
	require.NoError(err)
	require.Len(results, 3)

	var firstPubKey curve.Point
	for id, result := range results {
		config, ok := result.(*cmp.Config)
		require.True(ok, "result should be *cmp.Config for party %s", id)
		require.NotNil(config)

		pubKey := config.PublicPoint()
		if firstPubKey == nil {
			firstPubKey = pubKey
		} else {
			assert.True(t, firstPubKey.Equal(pubKey),
				"all parties should have same public key")
		}
	}

	t.Logf("CMP keygen completed: 3 parties, threshold=%d", threshold)
}

// TestFROSTSignFullExecution runs a complete FROST keygen + sign
func TestFROSTSignFullExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping FROST sign execution in short mode")
	}

	require := require.New(t)

	pIDs := testPartyIDs(5)
	threshold := 3
	signers := pIDs[:threshold]
	message := []byte("test message for threshold signing")

	// Keygen
	keygenResults, err := runTestProtocol(t, pIDs, func(id party.ID) protocol.StartFunc {
		return frost.Keygen(curve.Secp256k1{}, id, pIDs, threshold)
	})
	require.NoError(err)
	require.Len(keygenResults, 5)

	firstConfig := keygenResults[pIDs[0]].(*frost.Config)
	publicKey := firstConfig.PublicKey

	// Sign
	signResults, err := runTestProtocol(t, signers, func(id party.ID) protocol.StartFunc {
		config := keygenResults[id].(*frost.Config)
		return frost.Sign(config, signers, message)
	})
	require.NoError(err)
	require.Len(signResults, threshold)

	// Verify
	for _, result := range signResults {
		var sig *frost.Signature
		switch s := result.(type) {
		case *frost.Signature:
			sig = s
		case frost.Signature:
			sig = &s
		default:
			t.Fatalf("unexpected signature type: %T", result)
		}
		require.NotNil(sig)
		assert.True(t, sig.Verify(publicKey, message), "signature should verify")
		break
	}

	t.Logf("FROST sign completed: %d-of-%d threshold signature verified", threshold, len(pIDs))
}

// TestLSSSignFullExecution runs a complete LSS keygen + sign
func TestLSSSignFullExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LSS sign execution in short mode")
	}

	require := require.New(t)

	workerPool := pool.NewPool(4)
	defer workerPool.TearDown()

	pIDs := testPartyIDs(3)
	threshold := 2
	signers := pIDs[:threshold]
	message := []byte("test message for LSS threshold signing")

	// Keygen
	keygenResults, err := runTestProtocol(t, pIDs, func(id party.ID) protocol.StartFunc {
		return lss.Keygen(curve.Secp256k1{}, id, pIDs, threshold, workerPool)
	})
	require.NoError(err)
	require.Len(keygenResults, 3)

	// Sign
	signResults, err := runTestProtocol(t, signers, func(id party.ID) protocol.StartFunc {
		config := keygenResults[id].(*lss.Config)
		return lss.Sign(config, signers, message, workerPool)
	})
	require.NoError(err)
	require.Len(signResults, threshold)

	t.Logf("LSS sign completed: %d-of-%d threshold signature", threshold, len(pIDs))
}

// TestCMPSignFullExecution runs a complete CMP keygen + sign
func TestCMPSignFullExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CMP sign execution in short mode")
	}

	require := require.New(t)

	workerPool := pool.NewPool(4)
	defer workerPool.TearDown()

	pIDs := testPartyIDs(3)
	threshold := 2
	signers := pIDs[:threshold]
	message := []byte("test message for CMP threshold signing")

	// Keygen
	keygenResults, err := runTestProtocol(t, pIDs, func(id party.ID) protocol.StartFunc {
		return cmp.Keygen(curve.Secp256k1{}, id, pIDs, threshold, workerPool)
	})
	require.NoError(err)
	require.Len(keygenResults, 3)

	// Sign
	signResults, err := runTestProtocol(t, signers, func(id party.ID) protocol.StartFunc {
		config := keygenResults[id].(*cmp.Config)
		return cmp.Sign(config, signers, message, workerPool)
	})
	require.NoError(err)
	require.Len(signResults, threshold)

	t.Logf("CMP sign completed: %d-of-%d threshold signature", threshold, len(pIDs))
}

// TestProtocolExecutorWithRealKeygen tests ProtocolExecutor wrapper with real keygen
func TestProtocolExecutorWithRealKeygen(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	require := require.New(t)

	workerPool := pool.NewPool(4)
	defer workerPool.TearDown()
	logger := log.NewNoOpLogger()
	pe := NewProtocolExecutor(workerPool, logger)

	pIDs := testPartyIDs(3)
	threshold := 2

	startFunc := pe.LSSKeygenStartFunc(pIDs[0], pIDs, threshold)
	require.NotNil(startFunc)

	results, err := runTestProtocol(t, pIDs, func(id party.ID) protocol.StartFunc {
		return pe.LSSKeygenStartFunc(id, pIDs, threshold)
	})
	require.NoError(err)
	require.Len(results, 3)

	for id, result := range results {
		config, ok := result.(*lss.Config)
		require.True(ok, "result should be *lss.Config for party %s", id)

		share := &LSSKeyShare{Config: config}
		require.Equal(ProtocolLSS, share.Protocol())
		require.NotNil(share.PublicKey())
		require.Equal(id, share.PartyID())
		require.Equal(threshold, share.Threshold())
		require.Equal(3, share.TotalParties())
	}

	t.Logf("ProtocolExecutor+LSSKeyShare integration test passed")
}

// =============================================================================
// BLS Threshold Network Tests
// =============================================================================

// TestBLSThresholdSigningFullExecution tests BLS threshold keygen + signing
// Uses TrustedDealer for key generation and tests threshold signature aggregation
func TestBLSThresholdSigningFullExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BLS threshold signing in short mode")
	}

	require := require.New(t)

	pIDs := testPartyIDs(5)
	threshold := 3
	message := []byte("test message for BLS threshold signing")

	// Generate keys using TrustedDealer
	dealer := &bls.TrustedDealer{
		Threshold:    threshold,
		TotalParties: len(pIDs),
	}

	shares, groupPK, err := dealer.GenerateShares(context.Background(), pIDs)
	require.NoError(err)
	require.Len(shares, 5)
	require.NotNil(groupPK)

	// Get verification keys
	verificationKeys := bls.GetVerificationKeys(shares)
	require.Len(verificationKeys, 5)

	// Create configs for each party
	configs := make(map[party.ID]*bls.Config)
	for _, id := range pIDs {
		configs[id] = bls.NewConfig(id, threshold, len(pIDs), shares[id], groupPK, verificationKeys)
	}

	// Each party creates a partial signature
	sigShares := make([]*bls.SignatureShare, 0, threshold)
	for i := 0; i < threshold; i++ {
		id := pIDs[i]
		config := configs[id]

		share, err := config.Sign(message)
		require.NoError(err)
		require.NotNil(share)

		// Verify partial signature
		valid := config.VerifyPartialSignature(share, message)
		assert.True(t, valid, "partial signature from %s should be valid", id)

		sigShares = append(sigShares, share)
	}

	// Aggregate signatures
	aggregatedSig, err := bls.AggregateSignatures(sigShares, threshold)
	require.NoError(err)
	require.NotNil(aggregatedSig)

	// Verify aggregated signature against group public key
	valid := configs[pIDs[0]].VerifyAggregateSignature(message, aggregatedSig)
	assert.True(t, valid, "aggregated signature should verify against group public key")

	t.Logf("BLS threshold signing completed: %d-of-%d threshold signature verified", threshold, len(pIDs))
}

// TestBLSThresholdWithDifferentSignerSets tests that any t-of-n signers can produce valid signature
func TestBLSThresholdWithDifferentSignerSets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BLS signer set test in short mode")
	}

	require := require.New(t)

	pIDs := testPartyIDs(5)
	threshold := 3
	message := []byte("test message for different signer sets")

	// Generate keys
	dealer := &bls.TrustedDealer{
		Threshold:    threshold,
		TotalParties: len(pIDs),
	}

	shares, groupPK, err := dealer.GenerateShares(context.Background(), pIDs)
	require.NoError(err)

	verificationKeys := bls.GetVerificationKeys(shares)
	configs := make(map[party.ID]*bls.Config)
	for _, id := range pIDs {
		configs[id] = bls.NewConfig(id, threshold, len(pIDs), shares[id], groupPK, verificationKeys)
	}

	// Test multiple signer combinations
	signerSets := [][]party.ID{
		{pIDs[0], pIDs[1], pIDs[2]},       // First 3
		{pIDs[2], pIDs[3], pIDs[4]},       // Last 3
		{pIDs[0], pIDs[2], pIDs[4]},       // Every other
		{pIDs[1], pIDs[2], pIDs[3]},       // Middle 3
		{pIDs[0], pIDs[1], pIDs[2], pIDs[3]}, // More than threshold
	}

	for i, signers := range signerSets {
		sigShares := make([]*bls.SignatureShare, 0, len(signers))
		for _, id := range signers {
			share, err := configs[id].Sign(message)
			require.NoError(err)
			sigShares = append(sigShares, share)
		}

		aggregatedSig, err := bls.AggregateSignatures(sigShares, threshold)
		require.NoError(err)

		valid := configs[pIDs[0]].VerifyAggregateSignature(message, aggregatedSig)
		assert.True(t, valid, "signer set %d should produce valid signature", i)
	}

	t.Logf("BLS threshold with different signer sets: all %d sets verified", len(signerSets))
}

// =============================================================================
// Ringtail (Post-Quantum) Threshold Network Tests
// =============================================================================

// TestRingtailSessionInit tests Ringtail session initialization
// This verifies the protocol can be started without requiring full MPC execution.
func TestRingtailSessionInit(t *testing.T) {
	require := require.New(t)

	workerPool := pool.NewPool(4)
	defer workerPool.TearDown()

	pIDs := testPartyIDs(3)
	threshold := 2

	// Verify keygen session can be created
	for _, id := range pIDs {
		startFunc := ringtail.Keygen(id, pIDs, threshold, workerPool)
		require.NotNil(startFunc, "Keygen should return a function for %s", id)

		session, err := startFunc([]byte("test-session"))
		require.NoError(err)
		require.NotNil(session)
	}

	// Verify sign session can be created with mock config
	for _, id := range pIDs {
		cfg := &ringtail.Config{
			ID:           id,
			Threshold:    threshold,
			Participants: pIDs,
			PublicKey:    make([]byte, 32),
			PrivateShare: make([]byte, 32),
		}

		message := []byte("test message")
		signFunc := ringtail.Sign(cfg, pIDs[:threshold], message, workerPool)
		require.NotNil(signFunc, "Sign should return a function for %s", id)
	}

	// Verify refresh session can be created
	for _, id := range pIDs {
		cfg := &ringtail.Config{
			ID:           id,
			Threshold:    threshold,
			Participants: pIDs,
			PublicKey:    make([]byte, 32),
			PrivateShare: make([]byte, 32),
		}

		refreshFunc := ringtail.Refresh(cfg, pIDs, threshold, workerPool)
		require.NotNil(refreshFunc, "Refresh should return a function for %s", id)
	}

	t.Log("Ringtail session initialization verified for keygen, sign, and refresh")
}

// TestRingtailKeygenFullExecution tests Ringtail post-quantum threshold keygen
// NOTE: Ringtail MPC protocol rounds are under development. This test verifies
// session initialization works. Full execution test will be enabled once the
// lattice-based MPC rounds are complete.
func TestRingtailKeygenFullExecution(t *testing.T) {
	// Skip full execution test - Ringtail MPC rounds still under development
	t.Skip("Ringtail MPC keygen rounds under development - test session init only")

	require := require.New(t)

	workerPool := pool.NewPool(4)
	defer workerPool.TearDown()

	pIDs := testPartyIDs(3)
	threshold := 2

	results, err := runTestProtocol(t, pIDs, func(id party.ID) protocol.StartFunc {
		return ringtail.Keygen(id, pIDs, threshold, workerPool)
	})
	require.NoError(err)
	require.Len(results, 3)

	var firstPubKey []byte
	for id, result := range results {
		config, ok := result.(*ringtail.Config)
		require.True(ok, "result should be *ringtail.Config for party %s", id)
		require.NotNil(config)
		require.NotEmpty(config.PublicKey)

		if firstPubKey == nil {
			firstPubKey = config.PublicKey
		} else {
			assert.Equal(t, firstPubKey, config.PublicKey,
				"all parties should have same public key")
		}
	}

	t.Logf("Ringtail keygen completed: %d parties, threshold=%d", len(pIDs), threshold)
}

// TestRingtailSignFullExecution tests Ringtail keygen + sign
// NOTE: Depends on Ringtail keygen completing, which requires MPC rounds.
func TestRingtailSignFullExecution(t *testing.T) {
	// Skip until Ringtail MPC rounds are complete
	t.Skip("Ringtail MPC signing rounds under development")

	require := require.New(t)

	workerPool := pool.NewPool(4)
	defer workerPool.TearDown()

	pIDs := testPartyIDs(3)
	threshold := 2
	signers := pIDs[:threshold]
	message := []byte("test message for Ringtail post-quantum threshold signing")

	// Keygen
	keygenResults, err := runTestProtocol(t, pIDs, func(id party.ID) protocol.StartFunc {
		return ringtail.Keygen(id, pIDs, threshold, workerPool)
	})
	require.NoError(err)
	require.Len(keygenResults, 3)

	firstConfig := keygenResults[pIDs[0]].(*ringtail.Config)
	publicKey := firstConfig.PublicKey

	// Sign
	signResults, err := runTestProtocol(t, signers, func(id party.ID) protocol.StartFunc {
		config := keygenResults[id].(*ringtail.Config)
		return ringtail.Sign(config, signers, message, workerPool)
	})
	require.NoError(err)
	require.Len(signResults, threshold)

	// Verify signature using standalone verification
	for _, result := range signResults {
		sigBytes, ok := result.([]byte)
		if !ok {
			// Some protocols return different types
			continue
		}
		valid := ringtail.VerifySignature(publicKey, message, sigBytes)
		assert.True(t, valid, "Ringtail signature should verify")
		break
	}

	t.Logf("Ringtail sign completed: %d-of-%d post-quantum threshold signature", threshold, len(pIDs))
}

// TestRingtailRefreshFullExecution tests Ringtail share refresh protocol
// NOTE: Depends on Ringtail keygen completing, which requires MPC rounds.
func TestRingtailRefreshFullExecution(t *testing.T) {
	// Skip until Ringtail MPC rounds are complete
	t.Skip("Ringtail MPC refresh rounds under development")

	require := require.New(t)

	workerPool := pool.NewPool(4)
	defer workerPool.TearDown()

	pIDs := testPartyIDs(3)
	threshold := 2

	// Initial keygen
	keygenResults, err := runTestProtocol(t, pIDs, func(id party.ID) protocol.StartFunc {
		return ringtail.Keygen(id, pIDs, threshold, workerPool)
	})
	require.NoError(err)
	require.Len(keygenResults, 3)

	originalPubKey := keygenResults[pIDs[0]].(*ringtail.Config).PublicKey

	// Refresh shares (same threshold)
	refreshResults, err := runTestProtocol(t, pIDs, func(id party.ID) protocol.StartFunc {
		config := keygenResults[id].(*ringtail.Config)
		return ringtail.Refresh(config, pIDs, threshold, workerPool)
	})
	require.NoError(err)
	require.Len(refreshResults, 3)

	// Verify public key is preserved after refresh
	for id, result := range refreshResults {
		config, ok := result.(*ringtail.Config)
		require.True(ok, "refresh result should be *ringtail.Config for party %s", id)
		assert.Equal(t, originalPubKey, config.PublicKey,
			"public key should be preserved after refresh")
	}

	t.Logf("Ringtail refresh completed: shares updated, public key preserved")
}
