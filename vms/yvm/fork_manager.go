// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package yvm

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/logging"
)

var (
	errInvalidForkTransition = errors.New("invalid fork transition")
	errAssetNotMigrated      = errors.New("asset not migrated to target version")
	errVersionNotSupported   = errors.New("network version not supported")
)

// NetworkVersion represents a specific version/fork of the network
type NetworkVersion struct {
	VersionID       uint32    `json:"versionId"`
	Name            string    `json:"name"`
	ActivationEpoch uint64    `json:"activationEpoch"`
	ParentVersion   uint32    `json:"parentVersion"`
	Features        []string  `json:"features"`
	ChainStates     map[string][]byte `json:"chainStates"` // chainID -> state root
}

// ForkTransition represents a transition between network versions
type ForkTransition struct {
	FromVersion     uint32    `json:"fromVersion"`
	ToVersion       uint32    `json:"toVersion"`
	TransitionEpoch uint64    `json:"transitionEpoch"`
	MigrationRules  []MigrationRule `json:"migrationRules"`
	Status          string    `json:"status"` // pending, active, completed
}

// MigrationRule defines how assets migrate between versions
type MigrationRule struct {
	AssetType       string `json:"assetType"`       // LUX, NFT, Contract, etc.
	SourceChain     string `json:"sourceChain"`
	TargetChain     string `json:"targetChain"`
	ConversionRatio string `json:"conversionRatio"` // e.g., "1:1", "1000:1"
	RequiresClaim   bool   `json:"requiresClaim"`   // Manual claim vs automatic
}

// AssetMigration tracks an individual asset migration
type AssetMigration struct {
	MigrationID     ids.ID    `json:"migrationId"`
	AssetID         ids.ID    `json:"assetId"`
	Owner           ids.ShortID `json:"owner"`
	SourceVersion   uint32    `json:"sourceVersion"`
	TargetVersion   uint32    `json:"targetVersion"`
	Amount          uint64    `json:"amount"`
	MigrationProof  []byte    `json:"migrationProof"`
	ClaimedAt       *time.Time `json:"claimedAt,omitempty"`
	Status          string    `json:"status"` // pending, migrated, claimed
}

// QuantumState represents the superposition of network states
type QuantumState struct {
	ActiveVersions  []uint32           `json:"activeVersions"`
	StateVector     map[uint32]float64 `json:"stateVector"` // version -> probability
	Entanglements   []Entanglement     `json:"entanglements"`
	MeasurementTime time.Time          `json:"measurementTime"`
}

// Entanglement represents entangled states between versions
type Entanglement struct {
	Version1        uint32 `json:"version1"`
	Version2        uint32 `json:"version2"`
	EntanglementKey []byte `json:"entanglementKey"`
	Strength        float64 `json:"strength"` // 0.0 to 1.0
}

// ForkManager manages network versions and transitions
type ForkManager struct {
	versions        map[uint32]*NetworkVersion
	transitions     map[string]*ForkTransition // "v1->v2" format
	migrations      map[ids.ID]*AssetMigration
	quantumStates   map[uint64]*QuantumState // epoch -> quantum state
	
	currentVersion  uint32
	supportedVersions []uint32
	
	log logging.Logger
	mu  sync.RWMutex
}

// NewForkManager creates a new fork manager
func NewForkManager(log logging.Logger) *ForkManager {
	return &ForkManager{
		versions:      make(map[uint32]*NetworkVersion),
		transitions:   make(map[string]*ForkTransition),
		migrations:    make(map[ids.ID]*AssetMigration),
		quantumStates: make(map[uint64]*QuantumState),
		log:           log,
	}
}

// RegisterVersion registers a new network version
func (fm *ForkManager) RegisterVersion(version *NetworkVersion) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	if _, exists := fm.versions[version.VersionID]; exists {
		return fmt.Errorf("version %d already registered", version.VersionID)
	}
	
	// Verify parent version exists (except for genesis)
	if version.ParentVersion != 0 {
		if _, exists := fm.versions[version.ParentVersion]; !exists {
			return fmt.Errorf("parent version %d not found", version.ParentVersion)
		}
	}
	
	fm.versions[version.VersionID] = version
	fm.supportedVersions = append(fm.supportedVersions, version.VersionID)
	
	fm.log.Info("registered network version",
		zap.Uint32("versionID", version.VersionID),
		zap.String("name", version.Name),
		zap.Uint64("activationEpoch", version.ActivationEpoch),
	)
	
	return nil
}

// CreateForkTransition creates a transition between versions
func (fm *ForkManager) CreateForkTransition(transition *ForkTransition) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	// Verify versions exist
	if _, exists := fm.versions[transition.FromVersion]; !exists {
		return fmt.Errorf("source version %d not found", transition.FromVersion)
	}
	if _, exists := fm.versions[transition.ToVersion]; !exists {
		return fmt.Errorf("target version %d not found", transition.ToVersion)
	}
	
	key := fmt.Sprintf("%d->%d", transition.FromVersion, transition.ToVersion)
	fm.transitions[key] = transition
	
	fm.log.Info("created fork transition",
		zap.Uint32("fromVersion", transition.FromVersion),
		zap.Uint32("toVersion", transition.ToVersion),
		zap.Uint64("transitionEpoch", transition.TransitionEpoch),
	)
	
	return nil
}

// MigrateAsset creates an asset migration between versions
func (fm *ForkManager) MigrateAsset(
	assetID ids.ID,
	owner ids.ShortID,
	amount uint64,
	fromVersion, toVersion uint32,
) (*AssetMigration, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	// Verify transition exists
	key := fmt.Sprintf("%d->%d", fromVersion, toVersion)
	transition, exists := fm.transitions[key]
	if !exists {
		return nil, errInvalidForkTransition
	}
	
	// Create migration
	migration := &AssetMigration{
		MigrationID:   fm.generateMigrationID(assetID, owner, fromVersion, toVersion),
		AssetID:       assetID,
		Owner:         owner,
		SourceVersion: fromVersion,
		TargetVersion: toVersion,
		Amount:        amount,
		Status:        "pending",
	}
	
	// Generate migration proof
	migration.MigrationProof = fm.generateMigrationProof(migration, transition)
	
	fm.migrations[migration.MigrationID] = migration
	
	fm.log.Info("created asset migration",
		zap.Stringer("migrationID", migration.MigrationID),
		zap.Stringer("assetID", assetID),
		zap.Uint32("fromVersion", fromVersion),
		zap.Uint32("toVersion", toVersion),
	)
	
	return migration, nil
}

// UpdateQuantumState updates the quantum state for an epoch
func (fm *ForkManager) UpdateQuantumState(epoch uint64, chainStates map[string][]byte) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	// Calculate state vector based on active versions
	stateVector := make(map[uint32]float64)
	totalActivity := 0.0
	
	// Simple model: probability based on chain activity
	for versionID := range fm.versions {
		// In real implementation, this would analyze actual chain activity
		activity := float64(len(chainStates)) // Placeholder
		stateVector[versionID] = activity
		totalActivity += activity
	}
	
	// Normalize probabilities
	if totalActivity > 0 {
		for versionID := range stateVector {
			stateVector[versionID] /= totalActivity
		}
	}
	
	// Detect entanglements (versions sharing state)
	entanglements := fm.detectEntanglements(chainStates)
	
	quantumState := &QuantumState{
		ActiveVersions:  fm.supportedVersions,
		StateVector:     stateVector,
		Entanglements:   entanglements,
		MeasurementTime: time.Now(),
	}
	
	fm.quantumStates[epoch] = quantumState
	
	return nil
}

// GetMigrationPath finds the optimal migration path between versions
func (fm *ForkManager) GetMigrationPath(fromVersion, toVersion uint32) ([]uint32, error) {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	
	if fromVersion == toVersion {
		return []uint32{fromVersion}, nil
	}
	
	// Simple BFS to find path
	visited := make(map[uint32]bool)
	queue := [][]uint32{{fromVersion}}
	
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		
		current := path[len(path)-1]
		if current == toVersion {
			return path, nil
		}
		
		if visited[current] {
			continue
		}
		visited[current] = true
		
		// Check all possible transitions
		for key, transition := range fm.transitions {
			if transition.FromVersion == current && transition.Status == "active" {
				newPath := append([]uint32{}, path...)
				newPath = append(newPath, transition.ToVersion)
				queue = append(queue, newPath)
			}
		}
	}
	
	return nil, fmt.Errorf("no migration path from version %d to %d", fromVersion, toVersion)
}

// Helper functions

func (fm *ForkManager) generateMigrationID(
	assetID ids.ID,
	owner ids.ShortID,
	fromVersion, toVersion uint32,
) ids.ID {
	h := sha256.New()
	h.Write(assetID[:])
	h.Write(owner[:])
	binary.Write(h, binary.BigEndian, fromVersion)
	binary.Write(h, binary.BigEndian, toVersion)
	binary.Write(h, binary.BigEndian, time.Now().Unix())
	return ids.ID(h.Sum(nil))
}

func (fm *ForkManager) generateMigrationProof(
	migration *AssetMigration,
	transition *ForkTransition,
) []byte {
	h := sha256.New()
	h.Write(migration.MigrationID[:])
	h.Write(migration.AssetID[:])
	h.Write([]byte(fmt.Sprintf("%d->%d", migration.SourceVersion, migration.TargetVersion)))
	
	// In production, this would include Merkle proofs and signatures
	return h.Sum(nil)
}

func (fm *ForkManager) detectEntanglements(chainStates map[string][]byte) []Entanglement {
	// Simplified entanglement detection
	// In reality, this would analyze shared state roots and cross-chain references
	
	entanglements := []Entanglement{}
	
	// Detect if chains share state roots (indicating entanglement)
	stateMap := make(map[string][]string)
	for chainID, stateRoot := range chainStates {
		rootStr := fmt.Sprintf("%x", stateRoot)
		stateMap[rootStr] = append(stateMap[rootStr], chainID)
	}
	
	// Create entanglements for shared states
	for stateRoot, chains := range stateMap {
		if len(chains) > 1 {
			// Chains sharing state are entangled
			for i := 0; i < len(chains)-1; i++ {
				for j := i + 1; j < len(chains); j++ {
					entanglements = append(entanglements, Entanglement{
						Version1:        fm.currentVersion, // Simplified
						Version2:        fm.currentVersion,
						EntanglementKey: []byte(stateRoot),
						Strength:        0.8, // High entanglement for shared state
					})
				}
			}
		}
	}
	
	return entanglements
}

// ClaimMigration processes a migration claim
func (fm *ForkManager) ClaimMigration(migrationID ids.ID, claimer ids.ShortID) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	migration, exists := fm.migrations[migrationID]
	if !exists {
		return errors.New("migration not found")
	}
	
	if migration.Owner != claimer {
		return errors.New("unauthorized claimer")
	}
	
	if migration.Status == "claimed" {
		return errors.New("migration already claimed")
	}
	
	now := time.Now()
	migration.ClaimedAt = &now
	migration.Status = "claimed"
	
	fm.log.Info("migration claimed",
		zap.Stringer("migrationID", migrationID),
		zap.Stringer("claimer", claimer),
	)
	
	return nil
}

// GetQuantumState returns the quantum state for an epoch
func (fm *ForkManager) GetQuantumState(epoch uint64) (*QuantumState, error) {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	
	state, exists := fm.quantumStates[epoch]
	if !exists {
		return nil, errors.New("quantum state not found for epoch")
	}
	
	return state, nil
}