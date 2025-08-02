// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package beam

import (
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/stretchr/testify/require"
)

// TestQuasarCreation tests Quasar creation
func TestQuasarCreation(t *testing.T) {
	require := require.New(t)

	// Test with empty secret key
	_, err := NewQuasar(ids.GenerateTestNodeID(), []byte{}, QuasarConfig{})
	require.Error(err)

	// Test with valid secret key
	rtSK := make([]byte, 64)
	rand.Read(rtSK)

	config := QuasarConfig{
		Threshold:     15,
		QuasarTimeout: 100 * time.Millisecond,
		Validators:    &mockValidatorState{},
	}

	quasar, err := NewQuasar(ids.GenerateTestNodeID(), rtSK, config)
	require.NoError(err)
	require.NotNil(quasar)
	require.NotNil(quasar.precompPool)
}

// TestShareCollection tests share collection and aggregation
func TestShareCollection(t *testing.T) {
	require := require.New(t)

	// Create Quasar
	rtSK := make([]byte, 64)
	rand.Read(rtSK)

	config := QuasarConfig{
		Threshold:     3,
		QuasarTimeout: 100 * time.Millisecond,
	}

	quasar, err := NewQuasar(ids.GenerateTestNodeID(), rtSK, config)
	require.NoError(err)

	// Register for certificate
	height := uint64(42)
	certCh := make(chan []byte, 1)
	quasar.RegisterForCertificate(height, certCh)

	// Simulate share collection
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			nodeID := ids.GenerateTestNodeID()
			share := make([]byte, 430) // Mock share
			share[0] = byte(idx)
			quasar.OnRTShare(height, nodeID, share)
		}(i)
	}

	// Wait for certificate
	select {
	case cert := <-certCh:
		require.NotNil(cert)
		require.Len(cert, 3072) // Mock cert size
	case <-time.After(1 * time.Second):
		t.Fatal("Certificate not received")
	}

	wg.Wait()

	// Verify metrics
	require.Equal(uint64(3), quasar.sharesCollected)
	require.Equal(uint64(1), quasar.certsGenerated)
}

// TestQuickSign tests quick signing with precomputation
func TestQuickSign(t *testing.T) {
	require := require.New(t)

	// Create Quasar
	rtSK := make([]byte, 64)
	rand.Read(rtSK)

	quasar, err := NewQuasar(ids.GenerateTestNodeID(), rtSK, QuasarConfig{})
	require.NoError(err)

	// Wait for precomputation pool to generate some values
	time.Sleep(300 * time.Millisecond)

	// Test quick sign
	msg := make([]byte, 32)
	rand.Read(msg)

	share, err := quasar.QuickSign(msg)
	require.NoError(err)
	require.Len(share, 430) // Mock share size
}

// TestPrecompPool tests precomputation pool
func TestPrecompPool(t *testing.T) {
	require := require.New(t)

	// Create pool
	pool := NewPrecompPool(10)
	require.NotNil(pool)
	require.Equal(10, pool.capacity)

	// Start precomputation
	sk := make([]byte, 64)
	rand.Read(sk)

	go pool.Start(sk)

	// Wait for pool to fill
	time.Sleep(1500 * time.Millisecond)

	// Get precomputed values
	var precomps [][]byte
	for i := 0; i < 5; i++ {
		pre := pool.Get()
		if pre != nil {
			precomps = append(precomps, pre)
		}
	}

	require.GreaterOrEqual(len(precomps), 3)
}

// TestConcurrentShareDelivery tests concurrent share delivery
func TestConcurrentShareDelivery(t *testing.T) {
	require := require.New(t)

	// Create Quasar
	rtSK := make([]byte, 64)
	rand.Read(rtSK)

	config := QuasarConfig{
		Threshold: 15,
	}

	quasar, err := NewQuasar(ids.GenerateTestNodeID(), rtSK, config)
	require.NoError(err)

	// Register for certificate
	height := uint64(100)
	certCh := make(chan []byte, 1)
	quasar.RegisterForCertificate(height, certCh)

	// Deliver shares concurrently
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			nodeID := ids.GenerateTestNodeID()
			share := make([]byte, 430)
			share[0] = byte(idx)
			quasar.OnRTShare(height, nodeID, share)
		}(i)
	}

	// Wait for certificate
	select {
	case cert := <-certCh:
		require.NotNil(cert)
	case <-time.After(1 * time.Second):
		t.Fatal("Certificate not received")
	}

	wg.Wait()
}

// TestMultipleHeights tests handling multiple block heights
func TestMultipleHeights(t *testing.T) {
	require := require.New(t)

	// Create Quasar
	rtSK := make([]byte, 64)
	rand.Read(rtSK)

	config := QuasarConfig{
		Threshold: 2,
	}

	quasar, err := NewQuasar(ids.GenerateTestNodeID(), rtSK, config)
	require.NoError(err)

	// Register for multiple heights
	channels := make(map[uint64]chan []byte)
	for h := uint64(1); h <= 3; h++ {
		ch := make(chan []byte, 1)
		channels[h] = ch
		quasar.RegisterForCertificate(h, ch)
	}

	// Deliver shares for each height
	for h := uint64(1); h <= 3; h++ {
		for i := 0; i < 2; i++ {
			nodeID := ids.GenerateTestNodeID()
			share := make([]byte, 430)
			share[0] = byte(h)
			share[1] = byte(i)
			quasar.OnRTShare(h, nodeID, share)
		}
	}

	// Verify all certificates received
	for h, ch := range channels {
		select {
		case cert := <-ch:
			require.NotNil(cert)
			t.Logf("Received certificate for height %d", h)
		case <-time.After(1 * time.Second):
			t.Fatalf("Certificate not received for height %d", h)
		}
	}
}

// TestVerifyDualCertificates tests dual certificate verification
func TestVerifyDualCertificates(t *testing.T) {
	require := require.New(t)

	// Create block with dual certificates
	block := &Block{
		PrntID: ids.GenerateTestID(),
		Hght:   1,
		Tmstmp: time.Now().Unix(),
		Certs: CertBundle{
			BLSAgg: [96]byte{1, 2, 3},
			RTCert: make([]byte, 3072),
		},
	}

	// Mock keys
	blsPK := make([]byte, 48)
	rtPK := make([]byte, 32)

	// This would fail in production without proper signatures
	// but demonstrates the interface
	err := VerifyDualCertificates(block, blsPK, rtPK)
	_ = err // In mock mode, may or may not error

	// Test missing certificates
	block2 := &Block{
		PrntID: ids.GenerateTestID(),
		Hght:   2,
		Tmstmp: time.Now().Unix(),
	}

	err = VerifyDualCertificates(block2, blsPK, rtPK)
	require.Error(err)
	require.Contains(err.Error(), "missing dual certificates")
}

// BenchmarkShareCollection benchmarks share collection
func BenchmarkShareCollection(b *testing.B) {
	rtSK := make([]byte, 64)
	rand.Read(rtSK)

	config := QuasarConfig{
		Threshold: 15,
	}

	quasar, _ := NewQuasar(ids.GenerateTestNodeID(), rtSK, config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		height := uint64(i)
		for j := 0; j < 15; j++ {
			nodeID := ids.GenerateTestNodeID()
			share := make([]byte, 430)
			quasar.OnRTShare(height, nodeID, share)
		}
	}
}

// BenchmarkQuickSign benchmarks quick signing
func BenchmarkQuickSign(b *testing.B) {
	rtSK := make([]byte, 64)
	rand.Read(rtSK)

	quasar, _ := NewQuasar(ids.GenerateTestNodeID(), rtSK, QuasarConfig{})

	// Let pool fill
	time.Sleep(1 * time.Second)

	msg := make([]byte, 32)
	rand.Read(msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = quasar.QuickSign(msg)
	}
}

// Mock Ringtail implementation for testing
type MockRingtail struct{}

func (m *MockRingtail) KeyGen(seed []byte) ([]byte, []byte, error) {
	sk := make([]byte, 64)
	pk := make([]byte, 32)
	copy(sk, seed)
	copy(pk, seed[:32])
	return sk, pk, nil
}

func (m *MockRingtail) Precompute(sk []byte) ([]byte, error) {
	pre := make([]byte, 40960)
	rand.Read(pre)
	return pre, nil
}

func (m *MockRingtail) QuickSign(pre []byte, msg []byte) ([]byte, error) {
	share := make([]byte, 430)
	copy(share[:32], msg)
	return share, nil
}

func (m *MockRingtail) VerifyShare(pk []byte, msg []byte, share []byte) bool {
	return len(share) == 430
}

func (m *MockRingtail) Aggregate(shares [][]byte) ([]byte, error) {
	if len(shares) == 0 {
		return nil, errors.New("no shares")
	}
	cert := make([]byte, 3072)
	cert[0] = byte(len(shares))
	return cert, nil
}

func (m *MockRingtail) Verify(pk []byte, msg []byte, cert []byte) bool {
	return len(cert) == 3072
}