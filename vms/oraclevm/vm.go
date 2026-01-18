// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package oraclevm implements the Oracle Virtual Machine (O-Chain) for the Lux network.
// OracleVM provides decentralized oracle services for external data feeds.
//
// Key features:
//   - Observation: operators fetch data from external sources
//   - Commit: signed observations submitted to chain
//   - Aggregate: compute canonical output (median/TWAP/bounded deviation)
//   - ZK aggregation proofs for correctness
//   - Threshold attestation for compatibility fallback
package oraclevm

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/runtime"
	"github.com/luxfi/vm/chain"
	vmcore "github.com/luxfi/vm"

	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms/artifacts"
)

var (
	_ chain.ChainVM = (*VM)(nil)

	Version = &version.Semantic{
		Major: 1,
		Minor: 0,
		Patch: 0,
	}

	ErrNotInitialized     = errors.New("vm not initialized")
	ErrFeedNotFound       = errors.New("feed not found")
	ErrInvalidObservation = errors.New("invalid observation")
	ErrStaleObservation   = errors.New("stale observation")
	ErrInvalidAggregation = errors.New("invalid aggregation")
)

// Config contains OracleVM configuration
type Config struct {
	// Feed settings
	MaxFeedsPerBlock     int    `json:"maxFeedsPerBlock"`
	ObservationWindow    string `json:"observationWindow"`
	MinObservers         int    `json:"minObservers"`

	// Aggregation settings
	AggregationMethod    string `json:"aggregationMethod"` // median, twap, weighted
	DeviationThreshold   uint64 `json:"deviationThreshold"` // basis points

	// ZK settings
	EnableZKAggregation  bool   `json:"enableZkAggregation"`
	ZKProofSystem        string `json:"zkProofSystem"` // groth16, plonk

	// Attestation settings
	RequireQuorumCert    bool   `json:"requireQuorumCert"`
	QuorumThreshold      int    `json:"quorumThreshold"`
}

// DefaultConfig returns default OracleVM configuration
func DefaultConfig() Config {
	return Config{
		MaxFeedsPerBlock:    100,
		ObservationWindow:   "1m",
		MinObservers:        3,
		AggregationMethod:   "median",
		DeviationThreshold:  500, // 5%
		EnableZKAggregation: false,
		ZKProofSystem:       "groth16",
		RequireQuorumCert:   false,
		QuorumThreshold:     2,
	}
}

// Feed represents an oracle data feed
type Feed struct {
	ID           ids.ID            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Sources      []string          `json:"sources"`
	UpdateFreq   time.Duration     `json:"updateFreq"`
	PolicyHash   [32]byte          `json:"policyHash"`
	Operators    []ids.NodeID      `json:"operators"`
	CreatedAt    time.Time         `json:"createdAt"`
	Status       string            `json:"status"`
	Metadata     map[string]string `json:"metadata"`
}

// Observation represents a signed observation from an operator
type Observation struct {
	FeedID       ids.ID    `json:"feedId"`
	Value        []byte    `json:"value"`
	Timestamp    time.Time `json:"timestamp"`
	SourceMeta   [32]byte  `json:"sourceMetaHash"`
	OperatorID   ids.NodeID `json:"operatorId"`
	Signature    []byte    `json:"signature"`
}

// AggregatedValue represents the canonical output for a feed
type AggregatedValue struct {
	FeedID       ids.ID    `json:"feedId"`
	Epoch        uint64    `json:"epoch"`
	Value        []byte    `json:"value"`
	Timestamp    time.Time `json:"timestamp"`
	Observations int       `json:"observationCount"`
	AggProof     []byte    `json:"aggProof,omitempty"`
	QuorumCert   []byte    `json:"quorumCert,omitempty"`
}

// VM implements the Oracle Virtual Machine
type VM struct {
	rt     *runtime.Runtime
	config Config

	// Database
	db database.Database

	// Feed management
	feeds       map[ids.ID]*Feed
	feedsByName map[string]ids.ID

	// Observations pending aggregation
	pendingObs map[ids.ID][]*Observation

	// Aggregated values by epoch
	values map[ids.ID]map[uint64]*AggregatedValue

	// Block management
	lastAcceptedID ids.ID
	lastAccepted   *Block
	pendingBlocks  map[ids.ID]*Block

	// Consensus
	toEngine chan<- vmcore.Message

	// Logging
	log log.Logger

	mu      sync.RWMutex
	running bool
}

// Block represents an OracleVM block
type Block struct {
	ID_        ids.ID    `json:"id"`
	ParentID_  ids.ID    `json:"parentID"`
	Height_    uint64    `json:"height"`
	Timestamp_ time.Time `json:"timestamp"`

	// Oracle-specific data
	Observations  []*Observation     `json:"observations,omitempty"`
	Aggregations  []*AggregatedValue `json:"aggregations,omitempty"`
	FeedUpdates   []*Feed            `json:"feedUpdates,omitempty"`
	Attestations  []*artifacts.OracleAttestation `json:"attestations,omitempty"`

	bytes []byte
	vm    *VM
}

// Genesis represents the genesis state
type Genesis struct {
	Version   int    `json:"version"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
	InitialFeeds []*Feed `json:"initialFeeds,omitempty"`
}

// Initialize initializes the VM with the unified Init struct
func (vm *VM) Initialize(ctx context.Context, init vmcore.Init) error {
	vm.rt = init.Runtime
	vm.db = init.DB
	vm.toEngine = init.ToEngine

	if logger, ok := vm.rt.Log.(log.Logger); ok {
		vm.log = logger
	} else {
		return errors.New("invalid logger type")
	}

	vm.feeds = make(map[ids.ID]*Feed)
	vm.feedsByName = make(map[string]ids.ID)
	vm.pendingObs = make(map[ids.ID][]*Observation)
	vm.values = make(map[ids.ID]map[uint64]*AggregatedValue)
	vm.pendingBlocks = make(map[ids.ID]*Block)

	// Parse configuration
	if len(init.Config) > 0 {
		if err := json.Unmarshal(init.Config, &vm.config); err != nil {
			return fmt.Errorf("failed to parse config: %w", err)
		}
	} else {
		vm.config = DefaultConfig()
	}

	// Parse genesis
	genesis := &Genesis{}
	if len(init.Genesis) > 0 {
		if err := json.Unmarshal(init.Genesis, genesis); err != nil {
			return fmt.Errorf("failed to parse genesis: %w", err)
		}
	}

	// Register initial feeds
	for _, feed := range genesis.InitialFeeds {
		vm.feeds[feed.ID] = feed
		vm.feedsByName[feed.Name] = feed.ID
		vm.values[feed.ID] = make(map[uint64]*AggregatedValue)
	}

	// Create genesis block
	genesisBlock := &Block{
		ID_:        ids.Empty,
		ParentID_:  ids.Empty,
		Height_:    0,
		Timestamp_: time.Unix(genesis.Timestamp, 0),
		vm:         vm,
	}
	genesisBlock.ID_ = genesisBlock.computeID()
	vm.lastAcceptedID = genesisBlock.ID_
	vm.lastAccepted = genesisBlock

	vm.running = true
	if !vm.log.IsZero() {
		vm.log.Info("OracleVM initialized",
			log.Int("feeds", len(vm.feeds)),
			log.String("aggregation", vm.config.AggregationMethod),
			log.Bool("zkEnabled", vm.config.EnableZKAggregation),
		)
	}

	return nil
}

// RegisterFeed registers a new oracle feed
func (vm *VM) RegisterFeed(feed *Feed) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if !vm.running {
		return ErrNotInitialized
	}

	if _, exists := vm.feedsByName[feed.Name]; exists {
		return fmt.Errorf("feed %s already exists", feed.Name)
	}

	feed.CreatedAt = time.Now()
	feed.Status = "active"
	vm.feeds[feed.ID] = feed
	vm.feedsByName[feed.Name] = feed.ID
	vm.values[feed.ID] = make(map[uint64]*AggregatedValue)

	return nil
}

// SubmitObservation submits an observation for a feed
func (vm *VM) SubmitObservation(obs *Observation) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if !vm.running {
		return ErrNotInitialized
	}

	feed, exists := vm.feeds[obs.FeedID]
	if !exists {
		return ErrFeedNotFound
	}

	// Validate observation freshness
	window, _ := time.ParseDuration(vm.config.ObservationWindow)
	if time.Since(obs.Timestamp) > window {
		return ErrStaleObservation
	}

	// Validate operator is authorized
	authorized := false
	for _, op := range feed.Operators {
		if op == obs.OperatorID {
			authorized = true
			break
		}
	}
	if !authorized {
		return fmt.Errorf("operator %s not authorized for feed %s", obs.OperatorID, feed.Name)
	}

	// Add to pending observations
	vm.pendingObs[obs.FeedID] = append(vm.pendingObs[obs.FeedID], obs)

	return nil
}

// GetFeed returns a feed by ID
func (vm *VM) GetFeed(feedID ids.ID) (*Feed, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	if !vm.running {
		return nil, ErrNotInitialized
	}

	feed, exists := vm.feeds[feedID]
	if !exists {
		return nil, ErrFeedNotFound
	}

	return feed, nil
}

// GetLatestValue returns the latest aggregated value for a feed
func (vm *VM) GetLatestValue(feedID ids.ID) (*AggregatedValue, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	if !vm.running {
		return nil, ErrNotInitialized
	}

	epochs, exists := vm.values[feedID]
	if !exists || len(epochs) == 0 {
		return nil, ErrFeedNotFound
	}

	// Find latest epoch
	var latest *AggregatedValue
	var latestEpoch uint64
	for epoch, val := range epochs {
		if epoch > latestEpoch {
			latestEpoch = epoch
			latest = val
		}
	}

	return latest, nil
}

// CreateAttestation creates an OracleAttestation artifact
func (vm *VM) CreateAttestation(feedID ids.ID, epoch uint64) (*artifacts.OracleAttestation, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	if !vm.running {
		return nil, ErrNotInitialized
	}

	epochs, exists := vm.values[feedID]
	if !exists {
		return nil, ErrFeedNotFound
	}

	val, exists := epochs[epoch]
	if !exists {
		return nil, fmt.Errorf("no value for epoch %d", epoch)
	}

	feed := vm.feeds[feedID]

	att := &artifacts.OracleAttestation{
		Version_:   1,
		SigSuite_:  artifacts.SuiteHybrid,
		DomainID_:  vm.rt.ChainID,
		FeedID:     feedID,
		Epoch:      epoch,
		Value:      val.Value,
		AggProof:   val.AggProof,
		QuorumCert: val.QuorumCert,
		ValidFrom:  val.Timestamp,
		ValidTo:    val.Timestamp.Add(feed.UpdateFreq * 2),
		PolicyHash: feed.PolicyHash,
	}

	return att, nil
}

// Shutdown shuts down the VM
func (vm *VM) Shutdown(ctx context.Context) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if !vm.running {
		return nil
	}

	vm.running = false
	return nil
}

// CreateHandlers returns HTTP handlers
func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	return map[string]http.Handler{
		"/rpc": NewService(vm),
	}, nil
}

// Connected notifies the VM about connected nodes
func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *chain.VersionInfo) error {
	return nil
}

// Disconnected notifies the VM about disconnected nodes
func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	return nil
}

// =============================================================================
// ChainVM Interface Methods
// =============================================================================

func (vm *VM) SetState(ctx context.Context, state uint32) error {
	return nil
}

func (vm *VM) BuildBlock(ctx context.Context) (chain.Block, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if !vm.running {
		return nil, ErrNotInitialized
	}

	parent := vm.lastAccepted
	if parent == nil {
		return nil, errors.New("no parent block")
	}

	blk := &Block{
		ParentID_:  parent.ID_,
		Height_:    parent.Height_ + 1,
		Timestamp_: time.Now(),
		vm:         vm,
	}
	blk.ID_ = blk.computeID()

	vm.pendingBlocks[blk.ID_] = blk
	return blk, nil
}

func (vm *VM) ParseBlock(ctx context.Context, bytes []byte) (chain.Block, error) {
	blk := &Block{vm: vm}
	if err := json.Unmarshal(bytes, blk); err != nil {
		return nil, err
	}
	blk.ID_ = blk.computeID()
	return blk, nil
}

func (vm *VM) GetBlock(ctx context.Context, id ids.ID) (chain.Block, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	// Check pending blocks (nil-safe for early calls before initialization)
	if vm.pendingBlocks != nil {
		if blk, exists := vm.pendingBlocks[id]; exists {
			return blk, nil
		}
	}

	if vm.lastAccepted != nil && vm.lastAccepted.ID_ == id {
		return vm.lastAccepted, nil
	}

	bytes, err := vm.db.Get(id[:])
	if err != nil {
		return nil, err
	}

	blk := &Block{vm: vm}
	if err := json.Unmarshal(bytes, blk); err != nil {
		return nil, err
	}
	return blk, nil
}

func (vm *VM) SetPreference(ctx context.Context, id ids.ID) error {
	return nil
}

func (vm *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	return vm.lastAcceptedID, nil
}

func (vm *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	return ids.Empty, errors.New("height index not implemented")
}

func (vm *VM) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	handlers, err := vm.CreateHandlers(ctx)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	for path, handler := range handlers {
		if path == "" {
			path = "/"
		}
		mux.Handle(path, handler)
	}
	return mux, nil
}

func (vm *VM) Version(ctx context.Context) (string, error) {
	return Version.String(), nil
}

func (vm *VM) WaitForEvent(ctx context.Context) (vmcore.Message, error) {
	return vmcore.Message{}, nil
}

func (vm *VM) HealthCheck(ctx context.Context) (*chain.HealthResult, error) {
	return &chain.HealthResult{
		Healthy: vm.running,
		Details: map[string]string{
			"feeds": fmt.Sprintf("%d", len(vm.feeds)),
			"method": vm.config.AggregationMethod,
		},
	}, nil
}

// =============================================================================
// Block Methods
// =============================================================================

func (blk *Block) computeID() ids.ID {
	bytes, _ := json.Marshal(blk)
	hash := sha256.Sum256(bytes)
	return ids.ID(hash)
}

func (blk *Block) ID() ids.ID         { return blk.ID_ }
func (blk *Block) Parent() ids.ID     { return blk.ParentID_ }
func (blk *Block) ParentID() ids.ID   { return blk.ParentID_ }
func (blk *Block) Height() uint64     { return blk.Height_ }
func (blk *Block) Timestamp() time.Time { return blk.Timestamp_ }
func (blk *Block) Status() uint8      { return 0 }

func (blk *Block) Verify(ctx context.Context) error {
	return nil
}

func (blk *Block) Accept(ctx context.Context) error {
	blk.vm.mu.Lock()
	defer blk.vm.mu.Unlock()

	bytes, err := json.Marshal(blk)
	if err != nil {
		return err
	}
	if err := blk.vm.db.Put(blk.ID_[:], bytes); err != nil {
		return err
	}

	blk.vm.lastAcceptedID = blk.ID_
	blk.vm.lastAccepted = blk
	delete(blk.vm.pendingBlocks, blk.ID_)

	return nil
}

func (blk *Block) Reject(ctx context.Context) error {
	blk.vm.mu.Lock()
	defer blk.vm.mu.Unlock()
	delete(blk.vm.pendingBlocks, blk.ID_)
	return nil
}

func (blk *Block) Bytes() []byte {
	if blk.bytes == nil {
		blk.bytes, _ = json.Marshal(blk)
	}
	return blk.bytes
}
