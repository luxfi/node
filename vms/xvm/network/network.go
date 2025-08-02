// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar/engine/core"
	"github.com/luxfi/node/v2/quasar/engine/core/appsender"
	"github.com/luxfi/node/v2/quasar/validators"
	"github.com/luxfi/node/v2/network/p2p"
	"github.com/luxfi/node/v2/network/p2p/gossip"
	log "github.com/luxfi/log"
	"github.com/luxfi/node/v2/vms/txs/mempool"
	"github.com/luxfi/node/v2/vms/xvm/txs"
	"github.com/luxfi/node/v2/utils/set"
)

var (
	_ core.AppHandler      = (*Network)(nil)
	_ validators.Connector = (*Network)(nil)
)

// appSenderAdapter adapts core.AppSender to appsender.AppSender
type appSenderAdapter struct {
	sender core.AppSender
}

func (a *appSenderAdapter) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, message []byte) error {
	return a.sender.SendAppRequest(ctx, nodeIDs.List(), requestID, message)
}

func (a *appSenderAdapter) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, message []byte) error {
	return a.sender.SendAppResponse(ctx, nodeID, requestID, message)
}

func (a *appSenderAdapter) SendAppGossip(ctx context.Context, config appsender.SendConfig, message []byte) error {
	// For now, just send to all nodes without filtering
	return a.sender.SendAppGossip(ctx, message)
}

func (a *appSenderAdapter) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], message []byte) error {
	// Not supported by core.AppSender, so just use SendAppGossip
	return a.sender.SendAppGossip(ctx, message)
}

func (a *appSenderAdapter) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, message []byte) error {
	return a.sender.SendCrossChainAppRequest(ctx, chainID, requestID, message)
}

func (a *appSenderAdapter) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, message []byte) error {
	// Not supported by core.AppSender
	return nil
}

type Network struct {
	*p2p.Network

	log     log.Logger
	mempool *gossipMempool

	txPushGossiper        *gossip.PushGossiper[*txs.Tx]
	txPushGossipFrequency time.Duration
	txPullGossiper        gossip.Gossiper
	txPullGossipFrequency time.Duration
}

func New(
	log log.Logger,
	nodeID ids.NodeID,
	subnetID ids.ID,
	vdrs validators.State,
	parser txs.Parser,
	txVerifier TxVerifier,
	mempool mempool.Mempool[*txs.Tx],
	appSender core.AppSender,
	registerer prometheus.Registerer,
	config Config,
) (*Network, error) {
	adaptedSender := &appSenderAdapter{sender: appSender}
	p2pNetwork, err := p2p.NewNetwork(log, adaptedSender, registerer, "p2p")
	if err != nil {
		return nil, err
	}

	marshaller := &txParser{
		parser: parser,
	}
	validators := p2p.NewValidators(
		p2pNetwork.Peers,
		log,
		subnetID,
		vdrs,
		config.MaxValidatorSetStaleness,
	)
	txGossipClient := p2pNetwork.NewClient(
		p2p.TxGossipHandlerID,
		p2p.WithValidatorSampling(validators),
	)
	txGossipMetrics, err := gossip.NewMetrics(registerer, "tx")
	if err != nil {
		return nil, err
	}

	gossipMempool, err := newGossipMempool(
		mempool,
		registerer,
		log,
		txVerifier,
		config.ExpectedBloomFilterElements,
		config.ExpectedBloomFilterFalsePositiveProbability,
		config.MaxBloomFilterFalsePositiveProbability,
	)
	if err != nil {
		return nil, err
	}

	txPushGossiper, err := gossip.NewPushGossiper[*txs.Tx](
		marshaller,
		gossipMempool,
		validators,
		txGossipClient,
		txGossipMetrics,
		gossip.BranchingFactor{
			StakePercentage: config.PushGossipPercentStake,
			Validators:      config.PushGossipNumValidators,
			Peers:           config.PushGossipNumPeers,
		},
		gossip.BranchingFactor{
			Validators: config.PushRegossipNumValidators,
			Peers:      config.PushRegossipNumPeers,
		},
		config.PushGossipDiscardedCacheSize,
		config.TargetGossipSize,
		config.PushGossipMaxRegossipFrequency,
	)
	if err != nil {
		return nil, err
	}

	var txPullGossiper gossip.Gossiper = gossip.NewPullGossiper[*txs.Tx](
		log,
		marshaller,
		gossipMempool,
		txGossipClient,
		txGossipMetrics,
		config.PullGossipPollSize,
	)

	// Gossip requests are only served if a node is a validator
	txPullGossiper = gossip.ValidatorGossiper{
		Gossiper:   txPullGossiper,
		NodeID:     nodeID,
		Validators: validators,
	}

	handler := gossip.NewHandler[*txs.Tx](
		log,
		marshaller,
		gossipMempool,
		txGossipMetrics,
		config.TargetGossipSize,
	)

	validatorHandler := p2p.NewValidatorHandler(
		p2p.NewThrottlerHandler(
			handler,
			p2p.NewSlidingWindowThrottler(
				config.PullGossipThrottlingPeriod,
				config.PullGossipThrottlingLimit,
			),
			log,
		),
		validators,
		log,
	)

	// We allow pushing txs between all peers, but only serve gossip requests
	// from validators
	txGossipHandler := txGossipHandler{
		appGossipHandler:  handler,
		appRequestHandler: validatorHandler,
	}

	if err := p2pNetwork.AddHandler(p2p.TxGossipHandlerID, txGossipHandler); err != nil {
		return nil, err
	}

	return &Network{
		Network:               p2pNetwork,
		log:                   log,
		mempool:               gossipMempool,
		txPushGossiper:        txPushGossiper,
		txPushGossipFrequency: config.PushGossipFrequency,
		txPullGossiper:        txPullGossiper,
		txPullGossipFrequency: config.PullGossipFrequency,
	}, nil
}

func (n *Network) PushGossip(ctx context.Context) {
	gossip.Every(ctx, n.log, n.txPushGossiper, n.txPushGossipFrequency)
}

func (n *Network) PullGossip(ctx context.Context) {
	gossip.Every(ctx, n.log, n.txPullGossiper, n.txPullGossipFrequency)
}

// IssueTxFromRPC attempts to add a tx to the mempool, after verifying it. If
// the tx is added to the mempool, it will attempt to push gossip the tx to
// random peers in the network.
//
// If the tx is already in the mempool, mempool.ErrDuplicateTx will be
// returned.
// If the tx is not added to the mempool, an error will be returned.
func (n *Network) IssueTxFromRPC(tx *txs.Tx) error {
	if err := n.mempool.Add(tx); err != nil {
		return err
	}
	n.txPushGossiper.Add(tx)
	return nil
}

// IssueTxFromRPCWithoutVerification attempts to add a tx to the mempool,
// without first verifying it. If the tx is added to the mempool, it will
// attempt to push gossip the tx to random peers in the network.
//
// If the tx is already in the mempool, mempool.ErrDuplicateTx will be
// returned.
// If the tx is not added to the mempool, an error will be returned.
func (n *Network) IssueTxFromRPCWithoutVerification(tx *txs.Tx) error {
	if err := n.mempool.AddWithoutVerification(tx); err != nil {
		return err
	}
	n.txPushGossiper.Add(tx)
	return nil
}
