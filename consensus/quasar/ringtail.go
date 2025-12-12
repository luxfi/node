// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/lattice/v6/ring"
	"github.com/luxfi/lattice/v6/utils/sampling"
	"github.com/luxfi/lattice/v6/utils/structs"
	"github.com/luxfi/log"
	"github.com/luxfi/ringtail/primitives"
	"github.com/luxfi/ringtail/sign"
)

var (
	ErrRingtailNotInitialized = errors.New("ringtail not initialized")
	ErrRingtailRound1Failed   = errors.New("ringtail round 1 failed")
	ErrRingtailRound2Failed   = errors.New("ringtail round 2 failed")
	ErrRingtailVerifyFailed   = errors.New("ringtail signature verification failed")
	ErrRingtailMACFailed      = errors.New("ringtail MAC verification failed")
)

// RingtailConfig holds configuration for Ringtail threshold signatures
type RingtailConfig struct {
	NumParties int // Total number of parties (validators)
	Threshold  int // Minimum signers required (typically 2/3 + 1)
}

// RingtailParty represents a validator's Ringtail signing state
type RingtailParty struct {
	ID      int
	Party   *sign.Party
	SkShare structs.Vector[ring.Poly]
	Seeds   map[int][][]byte
	MACKeys map[int][]byte
	Lambda  ring.Poly // Lagrange coefficient
}

// RingtailCoordinator manages the threshold signing protocol
type RingtailCoordinator struct {
	mu     sync.RWMutex
	log    log.Logger
	config RingtailConfig

	// Ring parameters
	ring    *ring.Ring
	ringXi  *ring.Ring
	ringNu  *ring.Ring
	sampler *ring.UniformSampler

	// Public parameters (shared by all parties)
	A structs.Matrix[ring.Poly]
	B structs.Vector[ring.Poly]

	// Parties (validators)
	parties  map[ids.NodeID]*RingtailParty
	partyIDs []int
	nodeToID map[ids.NodeID]int
	idToNode map[int]ids.NodeID

	// Lagrange coefficients
	lagrangeCoeffs structs.Vector[ring.Poly]

	// Session state
	sessionID   int
	initialized bool
}

// NewRingtailCoordinator creates a new Ringtail threshold coordinator
func NewRingtailCoordinator(log log.Logger, config RingtailConfig) (*RingtailCoordinator, error) {
	if config.NumParties < 3 {
		return nil, errors.New("need at least 3 parties for threshold signatures")
	}
	if config.Threshold < 2 {
		return nil, errors.New("threshold must be at least 2")
	}
	if config.Threshold > config.NumParties {
		return nil, errors.New("threshold cannot exceed number of parties")
	}

	// Set global config in sign package
	sign.K = config.NumParties
	sign.Threshold = config.Threshold

	// Initialize rings
	r, err := ring.NewRing(1<<sign.LogN, []uint64{sign.Q})
	if err != nil {
		return nil, fmt.Errorf("failed to create ring: %w", err)
	}
	rXi, err := ring.NewRing(1<<sign.LogN, []uint64{sign.QXi})
	if err != nil {
		return nil, fmt.Errorf("failed to create ring_xi: %w", err)
	}
	rNu, err := ring.NewRing(1<<sign.LogN, []uint64{sign.QNu})
	if err != nil {
		return nil, fmt.Errorf("failed to create ring_nu: %w", err)
	}

	// Create sampler
	randomKey := make([]byte, sign.KeySize)
	if _, err := rand.Read(randomKey); err != nil {
		return nil, fmt.Errorf("failed to generate random key: %w", err)
	}
	prng, err := sampling.NewKeyedPRNG(randomKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create PRNG: %w", err)
	}
	sampler := ring.NewUniformSampler(prng, r)

	return &RingtailCoordinator{
		log:      log,
		config:   config,
		ring:     r,
		ringXi:   rXi,
		ringNu:   rNu,
		sampler:  sampler,
		parties:  make(map[ids.NodeID]*RingtailParty),
		nodeToID: make(map[ids.NodeID]int),
		idToNode: make(map[int]ids.NodeID),
	}, nil
}

// Initialize generates keys and distributes shares to all parties
// This is the "trusted dealer" phase - in production this would be DKG
func (rc *RingtailCoordinator) Initialize(validators []ids.NodeID) error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if len(validators) != rc.config.NumParties {
		return fmt.Errorf("expected %d validators, got %d", rc.config.NumParties, len(validators))
	}

	// Map node IDs to party IDs
	rc.partyIDs = make([]int, len(validators))
	for i, nodeID := range validators {
		rc.partyIDs[i] = i
		rc.nodeToID[nodeID] = i
		rc.idToNode[i] = nodeID
	}

	// Compute Lagrange coefficients
	rc.lagrangeCoeffs = primitives.ComputeLagrangeCoefficients(
		rc.ring, rc.partyIDs, big.NewInt(int64(sign.Q)),
	)

	// Generate dealer key
	dealerKey := make([]byte, sign.KeySize)
	if _, err := rand.Read(dealerKey); err != nil {
		return fmt.Errorf("failed to generate dealer key: %w", err)
	}

	// Generate public params and key shares
	A, skShares, seeds, macKeys, b := sign.Gen(
		rc.ring, rc.ringXi, rc.sampler, dealerKey, rc.lagrangeCoeffs,
	)
	rc.A = A
	rc.B = b

	// Create party instances for each validator
	for i, nodeID := range validators {
		prng, _ := sampling.NewKeyedPRNG(dealerKey)
		sampler := ring.NewUniformSampler(prng, rc.ring)

		party := sign.NewParty(i, rc.ring, rc.ringXi, rc.ringNu, sampler)
		party.SkShare = skShares[i]
		party.Seed = seeds
		party.MACKeys = macKeys[i]

		// Precompute Lagrange coefficient in NTT form
		lambda := rc.ring.NewPoly()
		lambda.Copy(rc.lagrangeCoeffs[i])
		rc.ring.NTT(lambda, lambda)
		rc.ring.MForm(lambda, lambda)
		party.Lambda = lambda

		rc.parties[nodeID] = &RingtailParty{
			ID:      i,
			Party:   party,
			SkShare: skShares[i],
			Seeds:   seeds,
			MACKeys: macKeys[i],
			Lambda:  lambda,
		}
	}

	rc.initialized = true
	rc.log.Info("ringtail coordinator initialized",
		"parties", rc.config.NumParties,
		"threshold", rc.config.Threshold,
	)

	return nil
}

// RingtailRound1Result holds the output of round 1 for a party
type RingtailRound1Result struct {
	PartyID int
	NodeID  ids.NodeID
	D       structs.Matrix[ring.Poly]
	MACs    map[int][]byte
}

// RingtailSignature represents a complete threshold signature
type RingtailSignature struct {
	C       ring.Poly
	Z       structs.Vector[ring.Poly]
	Delta   structs.Vector[ring.Poly]
	Signers []ids.NodeID
}

// SignRound1 executes round 1 of the signing protocol for a party
func (rc *RingtailCoordinator) SignRound1(nodeID ids.NodeID, message []byte) (*RingtailRound1Result, error) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	if !rc.initialized {
		return nil, ErrRingtailNotInitialized
	}

	party, ok := rc.parties[nodeID]
	if !ok {
		return nil, fmt.Errorf("unknown party: %s", nodeID)
	}

	// Generate PRF key from message
	prfKey := primitives.GenerateRandomSeed()

	// Execute round 1
	D, macs := party.Party.SignRound1(rc.A, rc.sessionID, []byte(prfKey), rc.partyIDs)

	return &RingtailRound1Result{
		PartyID: party.ID,
		NodeID:  nodeID,
		D:       D,
		MACs:    macs,
	}, nil
}

// SignRound2 executes round 2 of the signing protocol
func (rc *RingtailCoordinator) SignRound2(
	nodeID ids.NodeID,
	message string,
	round1Results map[ids.NodeID]*RingtailRound1Result,
) (structs.Vector[ring.Poly], error) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	if !rc.initialized {
		return nil, ErrRingtailNotInitialized
	}

	party, ok := rc.parties[nodeID]
	if !ok {
		return nil, fmt.Errorf("unknown party: %s", nodeID)
	}

	// Collect D matrices and MACs from all parties
	D := make(map[int]structs.Matrix[ring.Poly])
	MACs := make(map[int]map[int][]byte)
	for _, result := range round1Results {
		D[result.PartyID] = result.D
		MACs[result.PartyID] = result.MACs
	}

	// Verify MACs and compute aggregate
	valid, DSum, hash := party.Party.SignRound2Preprocess(rc.A, rc.B, D, MACs, rc.sessionID, rc.partyIDs)
	if !valid {
		return nil, ErrRingtailMACFailed
	}

	// Generate PRF key
	prfKey := primitives.GenerateRandomSeed()

	// Execute round 2
	z := party.Party.SignRound2(rc.A, rc.B, DSum, rc.sessionID, message, rc.partyIDs, []byte(prfKey), hash)

	return z, nil
}

// Finalize combines partial signatures into the final threshold signature
func (rc *RingtailCoordinator) Finalize(
	combinerNodeID ids.NodeID,
	zShares map[ids.NodeID]structs.Vector[ring.Poly],
) (*RingtailSignature, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if !rc.initialized {
		return nil, ErrRingtailNotInitialized
	}

	combiner, ok := rc.parties[combinerNodeID]
	if !ok {
		return nil, fmt.Errorf("unknown combiner: %s", combinerNodeID)
	}

	// Convert to party ID indexed map
	z := make(map[int]structs.Vector[ring.Poly])
	signers := make([]ids.NodeID, 0, len(zShares))
	for nodeID, zShare := range zShares {
		partyID := rc.nodeToID[nodeID]
		z[partyID] = zShare
		signers = append(signers, nodeID)
	}

	// Finalize signature
	c, zSum, delta := combiner.Party.SignFinalize(z, rc.A, rc.B)

	// Increment session for next signing
	rc.sessionID++

	return &RingtailSignature{
		C:       c,
		Z:       zSum,
		Delta:   delta,
		Signers: signers,
	}, nil
}

// Verify verifies a Ringtail threshold signature
func (rc *RingtailCoordinator) Verify(message string, sig *RingtailSignature) bool {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	if !rc.initialized || sig == nil {
		return false
	}

	return sign.Verify(rc.ring, rc.ringXi, rc.ringNu, sig.Z, rc.A, message, rc.B, sig.C, sig.Delta)
}

// GetPublicParams returns the public parameters (A, B) for external verification
func (rc *RingtailCoordinator) GetPublicParams() (structs.Matrix[ring.Poly], structs.Vector[ring.Poly]) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.A, rc.B
}

// Stats returns coordinator statistics
func (rc *RingtailCoordinator) Stats() RingtailStats {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	return RingtailStats{
		NumParties:    rc.config.NumParties,
		Threshold:     rc.config.Threshold,
		SessionID:     rc.sessionID,
		Initialized:   rc.initialized,
		ActiveParties: len(rc.parties),
	}
}

// RingtailStats contains coordinator statistics
type RingtailStats struct {
	NumParties    int
	Threshold     int
	SessionID     int
	Initialized   bool
	ActiveParties int
}

// ParallelSignRound1 executes round 1 for all parties in parallel
func (rc *RingtailCoordinator) ParallelSignRound1(message []byte) (map[ids.NodeID]*RingtailRound1Result, time.Duration, error) {
	rc.mu.RLock()
	if !rc.initialized {
		rc.mu.RUnlock()
		return nil, 0, ErrRingtailNotInitialized
	}
	parties := make([]ids.NodeID, 0, len(rc.parties))
	for nodeID := range rc.parties {
		parties = append(parties, nodeID)
	}
	rc.mu.RUnlock()

	start := time.Now()
	results := make(map[ids.NodeID]*RingtailRound1Result)
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, len(parties))

	for _, nodeID := range parties {
		wg.Add(1)
		go func(nid ids.NodeID) {
			defer wg.Done()
			result, err := rc.SignRound1(nid, message)
			if err != nil {
				errChan <- err
				return
			}
			mu.Lock()
			results[nid] = result
			mu.Unlock()
		}(nodeID)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			return nil, 0, err
		}
	}

	return results, time.Since(start), nil
}

// ParallelSignRound2 executes round 2 for all parties in parallel
func (rc *RingtailCoordinator) ParallelSignRound2(
	message string,
	round1Results map[ids.NodeID]*RingtailRound1Result,
) (map[ids.NodeID]structs.Vector[ring.Poly], time.Duration, error) {
	rc.mu.RLock()
	if !rc.initialized {
		rc.mu.RUnlock()
		return nil, 0, ErrRingtailNotInitialized
	}
	parties := make([]ids.NodeID, 0, len(rc.parties))
	for nodeID := range rc.parties {
		parties = append(parties, nodeID)
	}
	rc.mu.RUnlock()

	start := time.Now()
	results := make(map[ids.NodeID]structs.Vector[ring.Poly])
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, len(parties))

	for _, nodeID := range parties {
		wg.Add(1)
		go func(nid ids.NodeID) {
			defer wg.Done()
			z, err := rc.SignRound2(nid, message, round1Results)
			if err != nil {
				errChan <- err
				return
			}
			mu.Lock()
			results[nid] = z
			mu.Unlock()
		}(nodeID)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			return nil, 0, err
		}
	}

	return results, time.Since(start), nil
}

// IsInitialized returns whether the coordinator has been initialized
func (rc *RingtailCoordinator) IsInitialized() bool {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.initialized
}
