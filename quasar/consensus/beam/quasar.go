// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package beam

import (
	"crypto/sha256"
	"errors"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/crypto/ringtail"
	"github.com/luxfi/node/quasar/validators"
	"github.com/luxfi/log"
)

// Quasar manages quantum-secure consensus operations
type Quasar struct {
	// Configuration
	config QuasarConfig
	nodeID ids.NodeID
	rtSK   []byte

	// Precomputation
	precompPool *PrecompPool

	// Share management
	shareMu sync.RWMutex
	shares  map[uint64]map[ids.NodeID][]byte // height -> nodeID -> share

	// Certificate channels
	certMu   sync.RWMutex
	certChan map[uint64][]chan []byte // height -> channels waiting for cert

	// Metrics
	sharesCollected uint64
	certsGenerated  uint64
	timeouts        uint64

	log log.Logger
}

// QuasarConfig contains Quasar configuration
type QuasarConfig struct {
	Threshold     int
	QuasarTimeout time.Duration
	Validators    validators.State
}

// PrecompPool manages precomputed Ringtail data
type PrecompPool struct {
	mu       sync.Mutex
	pool     [][]byte
	capacity int
}

// NewQuasar creates a new Quasar instance
func NewQuasar(nodeID ids.NodeID, rtSK []byte, config QuasarConfig) (*Quasar, error) {
	if len(rtSK) == 0 {
		return nil, errors.New("empty Ringtail secret key")
	}

	q := &Quasar{
		config:   config,
		nodeID:   nodeID,
		rtSK:     rtSK,
		shares:   make(map[uint64]map[ids.NodeID][]byte),
		certChan: make(map[uint64][]chan []byte),
		log:      log.NewNoOpLogger(),
	}

	// Initialize precomputation pool
	q.precompPool = NewPrecompPool(64) // 64 precomputed shares
	go q.precompPool.Start(rtSK)

	return q, nil
}

// RegisterForCertificate registers to receive a certificate when ready
func (q *Quasar) RegisterForCertificate(height uint64, ch chan []byte) {
	q.certMu.Lock()
	defer q.certMu.Unlock()

	if q.certChan[height] == nil {
		q.certChan[height] = make([]chan []byte, 0)
	}
	q.certChan[height] = append(q.certChan[height], ch)
}

// OnRTShare handles incoming Ringtail shares
func (q *Quasar) OnRTShare(height uint64, nodeID ids.NodeID, share []byte) {
	q.shareMu.Lock()
	defer q.shareMu.Unlock()

	// Initialize height map if needed
	if q.shares[height] == nil {
		q.shares[height] = make(map[ids.NodeID][]byte)
	}

	// Store share
	q.shares[height][nodeID] = share
	q.sharesCollected++

	// Check if we have enough shares
	if len(q.shares[height]) >= q.config.Threshold {
		go q.aggregateShares(height)
	}
}

// aggregateShares aggregates shares into a certificate
func (q *Quasar) aggregateShares(height uint64) {
	q.shareMu.RLock()
	shares := make([][]byte, 0, len(q.shares[height]))
	for _, share := range q.shares[height] {
		shares = append(shares, share)
		if len(shares) >= q.config.Threshold {
			break
		}
	}
	q.shareMu.RUnlock()

	// Convert to ringtail.Share type
	rtShares := make([]ringtail.Share, len(shares))
	for i, share := range shares {
		rtShares[i] = ringtail.Share(share)
	}
	
	// Aggregate shares
	cert, err := ringtail.Aggregate(rtShares)
	if err != nil {
		q.log.Warn("failed to aggregate shares",
			"height", height,
			"error", err,
		)
		return
	}

	q.certsGenerated++

	// Notify waiters
	q.certMu.Lock()
	for _, ch := range q.certChan[height] {
		select {
		case ch <- cert:
		default:
			// Channel full or closed
		}
	}
	delete(q.certChan, height)
	q.certMu.Unlock()

	// Clean up shares
	q.shareMu.Lock()
	delete(q.shares, height)
	q.shareMu.Unlock()
}

// QuickSign creates a Ringtail share using precomputed data
func (q *Quasar) QuickSign(msg []byte) ([]byte, error) {
	pre := q.precompPool.Get()
	if pre == nil {
		// Fallback to regular signing
		return q.sign(msg)
	}

	hash := sha256.Sum256(msg)
	share, err := ringtail.QuickSign(pre, hash[:])
	if err != nil {
		return nil, err
	}

	return share, nil
}

// sign creates a Ringtail share without precomputation
func (q *Quasar) sign(msg []byte) ([]byte, error) {
	hash := sha256.Sum256(msg)
	share, err := ringtail.Sign(q.rtSK, hash[:])
	if err != nil {
		return nil, err
	}
	return share, nil
}

// NewPrecompPool creates a new precomputation pool
func NewPrecompPool(capacity int) *PrecompPool {
	return &PrecompPool{
		pool:     make([][]byte, 0, capacity),
		capacity: capacity,
	}
}

// Start starts precomputation
func (p *PrecompPool) Start(sk []byte) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		p.mu.Lock()
		if len(p.pool) < p.capacity {
			pre, err := ringtail.Precompute(sk)
			if err == nil {
				p.pool = append(p.pool, pre)
			}
		}
		p.mu.Unlock()
	}
}

// Get gets a precomputed value
func (p *PrecompPool) Get() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.pool) == 0 {
		return nil
	}

	pre := p.pool[len(p.pool)-1]
	p.pool = p.pool[:len(p.pool)-1]
	return pre
}

// VerifyDualCertificates verifies both BLS and RT certificates
func VerifyDualCertificates(block *Block, blsPK []byte, rtPK []byte) error {
	if !block.HasDualCert() {
		return errors.New("block missing dual certificates")
	}

	// Verify BLS signature
	// TODO: Implement actual BLS verification

	// Verify Ringtail certificate
	blockHash := block.ID()
	if !ringtail.Verify(rtPK, blockHash[:], block.Certs.RTCert) {
		return errors.New("Ringtail verification failed")
	}

	return nil
}