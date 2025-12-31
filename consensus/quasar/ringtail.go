// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/lattice/v7/ring"
	"github.com/luxfi/lattice/v7/utils/sampling"
	"github.com/luxfi/lattice/v7/utils/structs"
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

// Ensure RingtailCoordinator implements ThresholdSigner
var _ ThresholdSigner = (*RingtailCoordinator)(nil)

// ringtailParty represents a validator's Ringtail signing state (internal)
type ringtailParty struct {
	id      int
	party   *sign.Party
	skShare structs.Vector[ring.Poly]
	seeds   map[int][][]byte
	macKeys map[int][]byte
	lambda  ring.Poly
}

// ringtailRound1Result holds the output of round 1 for a party (internal)
type ringtailRound1Result struct {
	partyID int
	nodeID  ids.NodeID
	d       structs.Matrix[ring.Poly]
	macs    map[int][]byte
}

// internalSignature holds the internal signature representation (internal)
type internalSignature struct {
	c       ring.Poly
	z       structs.Vector[ring.Poly]
	delta   structs.Vector[ring.Poly]
	signers []ids.NodeID
}

// RingtailCoordinator manages the threshold signing protocol
type RingtailCoordinator struct {
	mu     sync.RWMutex
	log    log.Logger
	config RingtailConfig

	// Ring parameters (internal - lattice types hidden)
	ring_   *ring.Ring
	ringXi  *ring.Ring
	ringNu  *ring.Ring
	sampler *ring.UniformSampler

	// Public parameters (internal)
	a structs.Matrix[ring.Poly]
	b structs.Vector[ring.Poly]

	// Parties (validators)
	parties  map[ids.NodeID]*ringtailParty
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
		ring_:    r,
		ringXi:   rXi,
		ringNu:   rNu,
		sampler:  sampler,
		parties:  make(map[ids.NodeID]*ringtailParty),
		nodeToID: make(map[ids.NodeID]int),
		idToNode: make(map[int]ids.NodeID),
	}, nil
}

// Initialize sets up the threshold scheme with validators (implements ThresholdSigner)
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
		rc.ring_, rc.partyIDs, big.NewInt(int64(sign.Q)),
	)

	// Generate dealer key
	dealerKey := make([]byte, sign.KeySize)
	if _, err := rand.Read(dealerKey); err != nil {
		return fmt.Errorf("failed to generate dealer key: %w", err)
	}

	// Generate public params and key shares
	A, skShares, seeds, macKeys, B := sign.Gen(
		rc.ring_, rc.ringXi, rc.sampler, dealerKey, rc.lagrangeCoeffs,
	)
	rc.a = A
	rc.b = B

	// Create party instances for each validator
	for i, nodeID := range validators {
		prng, _ := sampling.NewKeyedPRNG(dealerKey)
		sampler := ring.NewUniformSampler(prng, rc.ring_)

		party := sign.NewParty(i, rc.ring_, rc.ringXi, rc.ringNu, sampler)
		party.SkShare = skShares[i]
		party.Seed = seeds
		party.MACKeys = macKeys[i]

		// Precompute Lagrange coefficient in NTT form
		lambda := rc.ring_.NewPoly()
		lambda.Copy(rc.lagrangeCoeffs[i])
		rc.ring_.NTT(lambda, lambda)
		rc.ring_.MForm(lambda, lambda)
		party.Lambda = lambda

		rc.parties[nodeID] = &ringtailParty{
			id:      i,
			party:   party,
			skShare: skShares[i],
			seeds:   seeds,
			macKeys: macKeys[i],
			lambda:  lambda,
		}
	}

	rc.initialized = true
	rc.log.Info("ringtail coordinator initialized",
		"parties", rc.config.NumParties,
		"threshold", rc.config.Threshold,
	)

	return nil
}

// Sign implements Signer interface - returns a RingtailSignature
func (rc *RingtailCoordinator) Sign(msg []byte) (Signature, error) {
	// Round 1: all parties generate commitments
	round1Results, _, err := rc.parallelSignRound1(msg)
	if err != nil {
		return nil, fmt.Errorf("round 1 failed: %w", err)
	}

	// Round 2: all parties generate partial signatures
	msgStr := string(msg)
	round2Results, _, err := rc.parallelSignRound2(msgStr, round1Results)
	if err != nil {
		return nil, fmt.Errorf("round 2 failed: %w", err)
	}

	// Finalize: combine partial signatures
	var combinerID ids.NodeID
	for nodeID := range rc.parties {
		combinerID = nodeID
		break
	}

	sig, err := rc.finalize(combinerID, round2Results)
	if err != nil {
		return nil, fmt.Errorf("finalize failed: %w", err)
	}

	// Convert internal signature to public RingtailSignature type
	sigBytes := rc.serializeInternalSignature(sig)
	return NewRingtailSignature(sigBytes, sig.signers), nil
}

// Type implements Signer interface
func (rc *RingtailCoordinator) Type() SignatureType {
	return SignatureTypeRingtail
}

// Threshold implements ThresholdSigner interface
func (rc *RingtailCoordinator) Threshold() int {
	return rc.config.Threshold
}

// NumParties implements ThresholdSigner interface
func (rc *RingtailCoordinator) NumParties() int {
	return rc.config.NumParties
}

// IsInitialized implements ThresholdSigner interface
func (rc *RingtailCoordinator) IsInitialized() bool {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.initialized
}

// Verify verifies a RingtailSignature
func (rc *RingtailCoordinator) Verify(msg []byte, sig Signature) bool {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	if !rc.initialized {
		return false
	}

	rtSig, ok := sig.(*RingtailSignature)
	if !ok {
		return false
	}

	internalSig, err := rc.deserializeInternalSignature(rtSig.Bytes())
	if err != nil {
		return false
	}

	return sign.Verify(rc.ring_, rc.ringXi, rc.ringNu, internalSig.z, rc.a, string(msg), rc.b, internalSig.c, internalSig.delta)
}

// Stats returns coordinator statistics
type RingtailStats struct {
	NumParties    int
	Threshold     int
	SessionID     int
	Initialized   bool
	ActiveParties int
}

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

// Internal methods - not exposed outside consensus package

func (rc *RingtailCoordinator) signRound1(nodeID ids.NodeID, message []byte) (*ringtailRound1Result, error) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	if !rc.initialized {
		return nil, ErrRingtailNotInitialized
	}

	party, ok := rc.parties[nodeID]
	if !ok {
		return nil, fmt.Errorf("unknown party: %s", nodeID)
	}

	prfKey := primitives.GenerateRandomSeed()
	D, macs := party.party.SignRound1(rc.a, rc.sessionID, []byte(prfKey), rc.partyIDs)

	return &ringtailRound1Result{
		partyID: party.id,
		nodeID:  nodeID,
		d:       D,
		macs:    macs,
	}, nil
}

func (rc *RingtailCoordinator) signRound2(
	nodeID ids.NodeID,
	message string,
	round1Results map[ids.NodeID]*ringtailRound1Result,
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

	D := make(map[int]structs.Matrix[ring.Poly])
	MACs := make(map[int]map[int][]byte)
	for _, result := range round1Results {
		D[result.partyID] = result.d
		MACs[result.partyID] = result.macs
	}

	valid, DSum, hash := party.party.SignRound2Preprocess(rc.a, rc.b, D, MACs, rc.sessionID, rc.partyIDs)
	if !valid {
		return nil, ErrRingtailMACFailed
	}

	prfKey := primitives.GenerateRandomSeed()
	z := party.party.SignRound2(rc.a, rc.b, DSum, rc.sessionID, message, rc.partyIDs, []byte(prfKey), hash)

	return z, nil
}

func (rc *RingtailCoordinator) finalize(
	combinerNodeID ids.NodeID,
	zShares map[ids.NodeID]structs.Vector[ring.Poly],
) (*internalSignature, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if !rc.initialized {
		return nil, ErrRingtailNotInitialized
	}

	combiner, ok := rc.parties[combinerNodeID]
	if !ok {
		return nil, fmt.Errorf("unknown combiner: %s", combinerNodeID)
	}

	z := make(map[int]structs.Vector[ring.Poly])
	signers := make([]ids.NodeID, 0, len(zShares))
	for nodeID, zShare := range zShares {
		partyID := rc.nodeToID[nodeID]
		z[partyID] = zShare
		signers = append(signers, nodeID)
	}

	c, zSum, delta := combiner.party.SignFinalize(z, rc.a, rc.b)
	rc.sessionID++

	return &internalSignature{
		c:       c,
		z:       zSum,
		delta:   delta,
		signers: signers,
	}, nil
}

func (rc *RingtailCoordinator) parallelSignRound1(message []byte) (map[ids.NodeID]*ringtailRound1Result, time.Duration, error) {
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
	results := make(map[ids.NodeID]*ringtailRound1Result)
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, len(parties))

	for _, nodeID := range parties {
		wg.Add(1)
		go func(nid ids.NodeID) {
			defer wg.Done()
			result, err := rc.signRound1(nid, message)
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

func (rc *RingtailCoordinator) parallelSignRound2(
	message string,
	round1Results map[ids.NodeID]*ringtailRound1Result,
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
			z, err := rc.signRound2(nid, message, round1Results)
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

// Serialization helpers - convert internal types to/from bytes

func (rc *RingtailCoordinator) serializeInternalSignature(sig *internalSignature) []byte {
	var buf []byte

	// Number of signers
	numSigners := make([]byte, 4)
	binary.BigEndian.PutUint32(numSigners, uint32(len(sig.signers)))
	buf = append(buf, numSigners...)

	// Signer IDs
	for _, signer := range sig.signers {
		buf = append(buf, signer[:]...)
	}

	// Serialize c polynomial
	cBytes := rc.serializePoly(sig.c)
	cLen := make([]byte, 4)
	binary.BigEndian.PutUint32(cLen, uint32(len(cBytes)))
	buf = append(buf, cLen...)
	buf = append(buf, cBytes...)

	// Serialize z vector
	zBytes := rc.serializeVector(sig.z)
	zLen := make([]byte, 4)
	binary.BigEndian.PutUint32(zLen, uint32(len(zBytes)))
	buf = append(buf, zLen...)
	buf = append(buf, zBytes...)

	// Serialize delta vector
	deltaBytes := rc.serializeVector(sig.delta)
	deltaLen := make([]byte, 4)
	binary.BigEndian.PutUint32(deltaLen, uint32(len(deltaBytes)))
	buf = append(buf, deltaLen...)
	buf = append(buf, deltaBytes...)

	return buf
}

func (rc *RingtailCoordinator) deserializeInternalSignature(data []byte) (*internalSignature, error) {
	if len(data) < 4 {
		return nil, errors.New("signature too short")
	}

	offset := 0

	numSigners := binary.BigEndian.Uint32(data[offset:])
	offset += 4

	signers := make([]ids.NodeID, numSigners)
	for i := uint32(0); i < numSigners; i++ {
		if offset+ids.NodeIDLen > len(data) {
			return nil, errors.New("signature truncated at signers")
		}
		copy(signers[i][:], data[offset:offset+ids.NodeIDLen])
		offset += ids.NodeIDLen
	}

	if offset+4 > len(data) {
		return nil, errors.New("signature truncated at c length")
	}
	cLen := binary.BigEndian.Uint32(data[offset:])
	offset += 4
	if offset+int(cLen) > len(data) {
		return nil, errors.New("signature truncated at c data")
	}
	c, err := rc.deserializePoly(data[offset : offset+int(cLen)])
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize c: %w", err)
	}
	offset += int(cLen)

	if offset+4 > len(data) {
		return nil, errors.New("signature truncated at z length")
	}
	zLen := binary.BigEndian.Uint32(data[offset:])
	offset += 4
	if offset+int(zLen) > len(data) {
		return nil, errors.New("signature truncated at z data")
	}
	z, err := rc.deserializeVector(data[offset : offset+int(zLen)])
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize z: %w", err)
	}
	offset += int(zLen)

	if offset+4 > len(data) {
		return nil, errors.New("signature truncated at delta length")
	}
	deltaLen := binary.BigEndian.Uint32(data[offset:])
	offset += 4
	if offset+int(deltaLen) > len(data) {
		return nil, errors.New("signature truncated at delta data")
	}
	delta, err := rc.deserializeVector(data[offset : offset+int(deltaLen)])
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize delta: %w", err)
	}

	return &internalSignature{
		c:       c,
		z:       z,
		delta:   delta,
		signers: signers,
	}, nil
}

func (rc *RingtailCoordinator) serializePoly(p ring.Poly) []byte {
	data, _ := p.MarshalBinary()
	return data
}

func (rc *RingtailCoordinator) deserializePoly(data []byte) (ring.Poly, error) {
	p := rc.ring_.NewPoly()
	if err := p.UnmarshalBinary(data); err != nil {
		return ring.Poly{}, err
	}
	return p, nil
}

func (rc *RingtailCoordinator) serializeVector(v structs.Vector[ring.Poly]) []byte {
	var buf []byte
	lenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBytes, uint32(len(v)))
	buf = append(buf, lenBytes...)
	for _, p := range v {
		pBytes := rc.serializePoly(p)
		// Prepend size for each poly
		sizeBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(sizeBytes, uint32(len(pBytes)))
		buf = append(buf, sizeBytes...)
		buf = append(buf, pBytes...)
	}
	return buf
}

func (rc *RingtailCoordinator) deserializeVector(data []byte) (structs.Vector[ring.Poly], error) {
	if len(data) < 4 {
		return nil, errors.New("vector data too short")
	}
	length := binary.BigEndian.Uint32(data[:4])
	offset := 4

	v := make(structs.Vector[ring.Poly], length)
	for i := uint32(0); i < length; i++ {
		if offset+4 > len(data) {
			return nil, errors.New("vector truncated at poly size")
		}
		polySize := int(binary.BigEndian.Uint32(data[offset:]))
		offset += 4
		if offset+polySize > len(data) {
			return nil, errors.New("vector truncated at poly data")
		}
		p, err := rc.deserializePoly(data[offset : offset+polySize])
		if err != nil {
			return nil, err
		}
		v[i] = p
		offset += polySize
	}
	return v, nil
}
