// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package zap provides ZAP (Zero-copy Agent Protocol) integration for Lux consensus.
//
// This package bridges ZAP's agentic consensus with Lux's Quasar threshold signatures,
// enabling W3C DID-based validator identity and post-quantum secure finality.
package zap

import (
	"context"
	"errors"
	"sync"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/consensus/quasar"
)

var (
	// ErrNotInitialized is returned when the bridge is used before initialization
	ErrNotInitialized = errors.New("zap bridge not initialized")
	// ErrValidatorNotFound is returned when a validator is not in the set
	ErrValidatorNotFound = errors.New("validator not found")
	// ErrQueryNotFound is returned when a query ID is not known
	ErrQueryNotFound = errors.New("query not found")
	// ErrAlreadyVoted is returned when a validator tries to vote twice
	ErrAlreadyVoted = errors.New("already voted on this query")
	// ErrInvalidDID is returned when a DID is malformed
	ErrInvalidDID = errors.New("invalid DID format")
)

// BridgeConfig configures the ZAP-Lux consensus bridge
type BridgeConfig struct {
	// ConsensusThreshold is the fraction of votes needed (0.5 = majority)
	ConsensusThreshold float64
	// MinResponses is the minimum responses before checking consensus
	MinResponses int
	// MinVotes is the minimum votes before checking consensus
	MinVotes int
	// EnablePQCrypto enables post-quantum signatures (ML-DSA-65)
	EnablePQCrypto bool
}

// DefaultBridgeConfig returns sensible defaults for the bridge
func DefaultBridgeConfig() BridgeConfig {
	return BridgeConfig{
		ConsensusThreshold: 0.5,
		MinResponses:       1,
		MinVotes:           3,
		EnablePQCrypto:     true,
	}
}

// Bridge connects ZAP agentic consensus to Lux's Quasar finality
type Bridge struct {
	log     log.Logger
	config  BridgeConfig
	quasar  *quasar.RingtailCoordinator
	mu      sync.RWMutex
	queries map[string]*QueryState // QueryID -> QueryState
	dids    map[ids.NodeID]*DID    // NodeID -> DID
}

// NewBridge creates a new ZAP-Lux consensus bridge
func NewBridge(log log.Logger, config BridgeConfig) *Bridge {
	return &Bridge{
		log:     log,
		config:  config,
		queries: make(map[string]*QueryState),
		dids:    make(map[ids.NodeID]*DID),
	}
}

// Initialize sets up the bridge with a Quasar coordinator
func (b *Bridge) Initialize(coordinator *quasar.RingtailCoordinator) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if coordinator == nil {
		return ErrNotInitialized
	}

	b.quasar = coordinator
	b.log.Info("ZAP bridge initialized",
		log.Int("threshold", coordinator.Threshold()),
		log.Int("parties", coordinator.NumParties()),
		log.Bool("pqCrypto", b.config.EnablePQCrypto),
	)
	return nil
}

// RegisterValidator associates a DID with a Lux NodeID
func (b *Bridge) RegisterValidator(nodeID ids.NodeID, did *DID) error {
	if did == nil || !did.Valid() {
		return ErrInvalidDID
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.dids[nodeID] = did
	b.log.Debug("Registered validator DID",
		log.Stringer("nodeID", nodeID),
		log.String("did", did.String()),
	)
	return nil
}

// GetValidatorDID returns the DID for a NodeID
func (b *Bridge) GetValidatorDID(nodeID ids.NodeID) (*DID, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	did, ok := b.dids[nodeID]
	if !ok {
		return nil, ErrValidatorNotFound
	}
	return did, nil
}

// SubmitQuery creates a new agentic consensus query
func (b *Bridge) SubmitQuery(ctx context.Context, queryID string, content []byte, submitter ids.NodeID) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.quasar == nil {
		return ErrNotInitialized
	}

	submitterDID, ok := b.dids[submitter]
	if !ok {
		return ErrValidatorNotFound
	}

	b.queries[queryID] = &QueryState{
		ID:        queryID,
		Content:   content,
		Submitter: submitterDID,
		Responses: make(map[string]*Response),
		Votes:     make(map[string][]*DID),
	}

	b.log.Debug("Query submitted",
		log.String("queryID", queryID),
		log.String("submitter", submitterDID.String()),
	)
	return nil
}

// SubmitResponse adds a response to a query
func (b *Bridge) SubmitResponse(ctx context.Context, queryID, responseID string, content []byte, responder ids.NodeID) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	state, ok := b.queries[queryID]
	if !ok {
		return ErrQueryNotFound
	}

	responderDID, ok := b.dids[responder]
	if !ok {
		return ErrValidatorNotFound
	}

	if state.Finalized != "" {
		return errors.New("query already finalized")
	}

	state.Responses[responseID] = &Response{
		ID:        responseID,
		QueryID:   queryID,
		Content:   content,
		Responder: responderDID,
	}
	state.Votes[responseID] = []*DID{}

	b.log.Debug("Response submitted",
		log.String("queryID", queryID),
		log.String("responseID", responseID),
		log.String("responder", responderDID.String()),
	)
	return nil
}

// Vote casts a vote for a response
func (b *Bridge) Vote(ctx context.Context, queryID, responseID string, voter ids.NodeID) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	state, ok := b.queries[queryID]
	if !ok {
		return ErrQueryNotFound
	}

	if state.Finalized != "" {
		return errors.New("query already finalized")
	}

	if _, ok := state.Responses[responseID]; !ok {
		return errors.New("response not found")
	}

	voterDID, ok := b.dids[voter]
	if !ok {
		return ErrValidatorNotFound
	}

	// Check for double voting (by DID URI)
	voterURI := voterDID.String()
	for _, voters := range state.Votes {
		for _, v := range voters {
			if v.String() == voterURI {
				return ErrAlreadyVoted
			}
		}
	}

	state.Votes[responseID] = append(state.Votes[responseID], voterDID)

	// Check consensus
	b.checkConsensus(state)

	return nil
}

func (b *Bridge) checkConsensus(state *QueryState) {
	if state.Finalized != "" {
		return
	}

	if len(state.Responses) < b.config.MinResponses {
		return
	}

	totalVotes := 0
	for _, voters := range state.Votes {
		totalVotes += len(voters)
	}

	if totalVotes < b.config.MinVotes {
		return
	}

	// Find best response
	var best struct {
		id    string
		count int
	}

	for responseID, voters := range state.Votes {
		count := len(voters)
		confidence := float64(count) / float64(totalVotes)

		if confidence >= b.config.ConsensusThreshold {
			if best.id == "" || count > best.count {
				best.id = responseID
				best.count = count
			}
		}
	}

	if best.id != "" {
		state.Finalized = best.id
		b.log.Info("Query finalized",
			log.String("queryID", state.ID),
			log.String("responseID", best.id),
			log.Int("votes", best.count),
			log.Int("total", totalVotes),
		)
	}
}

// GetResult returns the consensus result for a query
func (b *Bridge) GetResult(queryID string) (*ConsensusResult, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	state, ok := b.queries[queryID]
	if !ok {
		return nil, ErrQueryNotFound
	}

	if state.Finalized == "" {
		return nil, nil
	}

	response := state.Responses[state.Finalized]
	votes := len(state.Votes[state.Finalized])

	totalVoters := 0
	for _, v := range state.Votes {
		totalVoters += len(v)
	}

	confidence := 0.0
	if totalVoters > 0 {
		confidence = float64(votes) / float64(totalVoters)
	}

	return &ConsensusResult{
		Response:    response,
		Votes:       votes,
		TotalVoters: totalVoters,
		Confidence:  confidence,
	}, nil
}

// IsFinalized checks if a query has reached consensus
func (b *Bridge) IsFinalized(queryID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	state, ok := b.queries[queryID]
	return ok && state.Finalized != ""
}

// SignWithQuasar signs a message using Quasar hybrid signatures
func (b *Bridge) SignWithQuasar(msg []byte) (quasar.Signature, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.quasar == nil {
		return nil, ErrNotInitialized
	}

	return b.quasar.Sign(msg)
}

// VerifyQuasar verifies a Quasar signature
func (b *Bridge) VerifyQuasar(msg []byte, sig quasar.Signature) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.quasar == nil {
		return false
	}

	return b.quasar.Verify(msg, sig)
}

// Stats returns bridge statistics
func (b *Bridge) Stats() BridgeStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	active := 0
	finalized := 0
	for _, state := range b.queries {
		if state.Finalized != "" {
			finalized++
		} else {
			active++
		}
	}

	var quasarStats quasar.RingtailStats
	if b.quasar != nil {
		quasarStats = b.quasar.Stats()
	}

	return BridgeStats{
		RegisteredValidators: len(b.dids),
		ActiveQueries:        active,
		FinalizedQueries:     finalized,
		QuasarInitialized:    b.quasar != nil && b.quasar.IsInitialized(),
		QuasarStats:          quasarStats,
	}
}

// BridgeStats contains statistics about the bridge
type BridgeStats struct {
	RegisteredValidators int
	ActiveQueries        int
	FinalizedQueries     int
	QuasarInitialized    bool
	QuasarStats          quasar.RingtailStats
}
