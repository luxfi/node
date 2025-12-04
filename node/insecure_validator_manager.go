// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"sync"

	"github.com/luxfi/ids"
	"github.com/luxfi/consensus/networking/handler"
	"github.com/luxfi/consensus/networking/router"
	validators "github.com/luxfi/consensus/validator"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/log"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/proto/pb/p2p"
	"github.com/luxfi/node/utils/timer"
	"github.com/luxfi/node/version"
)

type insecureValidatorManager struct {
	router.Router
	log    log.Logger
	vdrs   validators.Manager
	weight uint64
	// Keep track of added validators locally
	validators map[ids.ID]map[ids.NodeID]uint64
	mu         sync.RWMutex
}

func (i *insecureValidatorManager) Connected(vdrID ids.NodeID, nodeVersion *version.Application, netID ids.ID) {
	if constants.PrimaryNetworkID == netID {
		// Track the validator locally since we can't modify the nodevalidators.Manager
		i.mu.Lock()
		if i.validators[netID] == nil {
			i.validators[netID] = make(map[ids.NodeID]uint64)
		}
		i.validators[netID][vdrID] = i.weight
		i.mu.Unlock()

		i.log.Debug("tracked validator connection",
			log.Stringer("nodeID", vdrID),
			log.Stringer("netID", netID),
			log.Uint64("weight", i.weight),
		)
	}
	// Forward to the underlying router
	// Note: Router.Connected is deprecated and not implemented
	// i.Router.Connected(vdrID, nodeVersion, netID)
}

func (i *insecureValidatorManager) Disconnected(vdrID ids.NodeID) {
	// Remove the validator from local tracking
	i.mu.Lock()
	if i.validators[constants.PrimaryNetworkID] != nil {
		delete(i.validators[constants.PrimaryNetworkID], vdrID)
	}
	i.mu.Unlock()

	i.log.Debug("removed validator",
		log.Stringer("nodeID", vdrID),
		log.Stringer("netID", constants.PrimaryNetworkID),
	)

	// Note: Router.Disconnected is deprecated and not implemented
	// i.Router.Disconnected(vdrID)
}

func (i *insecureValidatorManager) Deprecated() {
	// No-op - Router interface requirement
}

func (i *insecureValidatorManager) AddChain(ctx context.Context, chainID ids.ID, h handler.Handler) {
	// Stub - consensus router doesn't have AddChain
}

func (i *insecureValidatorManager) Benched(chainID ids.ID, nodeID ids.NodeID) {
	// Stub
}

func (i *insecureValidatorManager) Unbenched(chainID ids.ID, nodeID ids.NodeID) {
	// Stub
}

func (i *insecureValidatorManager) HealthCheck(ctx context.Context) (interface{}, error) {
	return nil, nil
}

func (i *insecureValidatorManager) Initialize(
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
	return nil
}

func (i *insecureValidatorManager) RegisterRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
	op message.Op,
	failedMsg message.InboundMessage,
	engineType p2p.EngineType,
) {
	// Stub
}

func (i *insecureValidatorManager) HandleInbound(ctx context.Context, msg message.InboundMessage) {
	// Stub
}

func (i *insecureValidatorManager) Shutdown(ctx context.Context) {
	// Stub
}
