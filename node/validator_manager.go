// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/luxfi/consensus/networking/handler"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/proto/p2p"
	"github.com/luxfi/node/version"
	"github.com/luxfi/timer"
)

var _ Router = (*ValidatorManager)(nil)

// ValidatorManager wraps the consensus router and handles:
// - Validator connection tracking
// - Beacon connection management for bootstrap
// - Sybil protection weight management
type ValidatorManager struct {
	Router
	log log.Logger

	// Validator tracking
	vdrs       validators.Manager
	validators map[ids.ID]map[ids.NodeID]uint64
	weight     uint64
	mu         sync.RWMutex

	// Tracked subnet IDs - validators are added to these on connect
	// when sybil protection is disabled
	trackedSubnets []ids.ID

	// Beacon tracking for bootstrap
	beacons                     validators.Manager
	requiredConns               int64
	numConns                    int64
	onSufficientlyConnected     chan struct{}
	onceOnSufficientlyConnected sync.Once

	// Feature flags
	sybilProtectionDisabled bool
}

// ValidatorManagerConfig configures the validator manager
type ValidatorManagerConfig struct {
	Router                  Router
	Log                     log.Logger
	Validators              validators.Manager
	Beacons                 validators.Manager
	TrackedSubnets          []ids.ID
	SybilProtectionDisabled bool
	SybilProtectionWeight   uint64
	RequiredBeaconConns     int64
	OnSufficientlyConnected chan struct{}
}

// NewValidatorManager creates a new validator manager
func NewValidatorManager(cfg ValidatorManagerConfig) *ValidatorManager {
	return &ValidatorManager{
		Router:                  cfg.Router,
		log:                     cfg.Log,
		vdrs:                    cfg.Validators,
		validators:              make(map[ids.ID]map[ids.NodeID]uint64),
		weight:                  cfg.SybilProtectionWeight,
		trackedSubnets:          cfg.TrackedSubnets,
		beacons:                 cfg.Beacons,
		requiredConns:           cfg.RequiredBeaconConns,
		onSufficientlyConnected: cfg.OnSufficientlyConnected,
		sybilProtectionDisabled: cfg.SybilProtectionDisabled,
	}
}

func (v *ValidatorManager) Connected(nodeID ids.NodeID, nodeVersion *version.Application, netID ids.ID) {
	// When sybil protection is disabled, add connecting peers as validators
	// so they can participate in consensus voting
	if v.sybilProtectionDisabled && constants.PrimaryNetworkID == netID {
		// Add to the actual validator manager so network layer can find them
		// Generate a dummy txID from the nodeID (same pattern as node.go line 585-586)
		dummyTxID := ids.Empty
		copy(dummyTxID[:], nodeID.Bytes())

		// Add to primary network
		err := v.vdrs.AddStaker(
			constants.PrimaryNetworkID,
			nodeID,
			nil, // No BLS public key for dynamically discovered validators
			dummyTxID,
			v.weight,
		)
		if err != nil {
			v.log.Warn("failed to add validator on connect",
				log.Stringer("nodeID", nodeID),
				log.Reflect("error", err),
			)
		} else {
			v.log.Info("added validator on connect (sybil protection disabled)",
				log.Stringer("nodeID", nodeID),
				log.Stringer("netID", netID),
				log.Uint64("weight", v.weight),
			)
		}

		// Also add to ALL tracked subnet validator sets so subnet consensus
		// engines can find validators for their chains. Without this, subnet
		// chains can't gossip blocks because the validator set is empty.
		for _, subnetID := range v.trackedSubnets {
			subnetTxID := ids.Empty
			copy(subnetTxID[:], nodeID.Bytes())
			if err := v.vdrs.AddStaker(subnetID, nodeID, nil, subnetTxID, v.weight); err != nil {
				v.log.Debug("failed to add subnet validator on connect",
					log.Stringer("nodeID", nodeID),
					log.Stringer("subnetID", subnetID),
					log.Reflect("error", err),
				)
			} else {
				v.log.Info("added subnet validator on connect (sybil protection disabled)",
					log.Stringer("nodeID", nodeID),
					log.Stringer("subnetID", subnetID),
				)
			}
		}

		// Also track locally for our records
		v.mu.Lock()
		if v.validators[netID] == nil {
			v.validators[netID] = make(map[ids.NodeID]uint64)
		}
		v.validators[netID][nodeID] = v.weight
		v.mu.Unlock()
	}

	// Track beacon connections for bootstrap
	if v.beacons != nil {
		_, isBeacon := v.beacons.GetValidator(constants.PrimaryNetworkID, nodeID)
		if isBeacon && constants.PrimaryNetworkID == netID {
			if atomic.AddInt64(&v.numConns, 1) >= v.requiredConns {
				v.onceOnSufficientlyConnected.Do(func() {
					if v.onSufficientlyConnected != nil {
						close(v.onSufficientlyConnected)
					}
				})
			}
		}
	}

	// Forward to underlying router
	v.Router.Connected(nodeID, nodeVersion, netID)
}

func (v *ValidatorManager) Disconnected(nodeID ids.NodeID) {
	// Remove from validator manager when sybil protection is disabled
	if v.sybilProtectionDisabled {
		// Remove from primary network
		err := v.vdrs.RemoveWeight(constants.PrimaryNetworkID, nodeID, v.weight)
		if err != nil {
			v.log.Debug("failed to remove validator weight on disconnect (may not exist)",
				log.Stringer("nodeID", nodeID),
				log.Reflect("error", err),
			)
		} else {
			v.log.Info("removed validator on disconnect (sybil protection disabled)",
				log.Stringer("nodeID", nodeID),
				log.Stringer("netID", constants.PrimaryNetworkID),
			)
		}

		// Also remove from all tracked subnet validator sets
		for _, subnetID := range v.trackedSubnets {
			if err := v.vdrs.RemoveWeight(subnetID, nodeID, v.weight); err != nil {
				v.log.Debug("failed to remove subnet validator on disconnect",
					log.Stringer("nodeID", nodeID),
					log.Stringer("subnetID", subnetID),
				)
			}
		}

		// Also remove from local tracking
		v.mu.Lock()
		if v.validators[constants.PrimaryNetworkID] != nil {
			delete(v.validators[constants.PrimaryNetworkID], nodeID)
		}
		v.mu.Unlock()
	}

	// Track beacon disconnections
	if v.beacons != nil {
		if _, isBeacon := v.beacons.GetValidator(constants.PrimaryNetworkID, nodeID); isBeacon {
			atomic.AddInt64(&v.numConns, -1)
		}
	}

	// Forward to underlying router
	v.Router.Disconnected(nodeID)
}

// Router interface methods - forward to underlying router
func (v *ValidatorManager) Deprecated() {}

func (v *ValidatorManager) AddChain(ctx context.Context, chainID ids.ID, h handler.Handler) {
	v.Router.AddChain(ctx, chainID, h)
}

func (v *ValidatorManager) Benched(chainID ids.ID, nodeID ids.NodeID) {
	v.Router.Benched(chainID, nodeID)
}

func (v *ValidatorManager) Unbenched(chainID ids.ID, nodeID ids.NodeID) {
	v.Router.Unbenched(chainID, nodeID)
}

func (v *ValidatorManager) HealthCheck(ctx context.Context) (interface{}, error) {
	return v.Router.HealthCheck(ctx)
}

func (v *ValidatorManager) Initialize(
	nodeID ids.NodeID,
	logger log.Logger,
	timeoutManager timer.AdaptiveTimeoutManager,
	gossipFrequency uint64,
	harshQuittersTime uint64,
	harshQuittersSlashingFraction uint64,
	appGossipValidatorSize uint64,
	appGossipNonValidatorSize uint64,
	gossipAcceptedFrontierSize uint64,
	appSendQueueSize uint64,
	peerNotConnectedF uint64,
	connectedPeers ...ids.NodeID,
) error {
	return v.Router.Initialize(
		nodeID,
		logger,
		timeoutManager,
		gossipFrequency,
		harshQuittersTime,
		harshQuittersSlashingFraction,
		appGossipValidatorSize,
		appGossipNonValidatorSize,
		gossipAcceptedFrontierSize,
		appSendQueueSize,
		peerNotConnectedF,
		connectedPeers...,
	)
}

func (v *ValidatorManager) RegisterRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
	op message.Op,
	failedMsg message.InboundMessage,
	engineType p2p.EngineType,
) {
	v.Router.RegisterRequest(ctx, nodeID, chainID, requestID, op, failedMsg, engineType)
}

func (v *ValidatorManager) HandleInbound(ctx context.Context, msg message.InboundMessage) {
	v.Router.HandleInbound(ctx, msg)
}

func (v *ValidatorManager) Shutdown(ctx context.Context) {
	v.Router.Shutdown(ctx)
}
