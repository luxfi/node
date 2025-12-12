// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"encoding/binary"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/luxfi/ids"
	validators "github.com/luxfi/consensus/validator"
	consensusversion "github.com/luxfi/consensus/version"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/metric"
	"github.com/luxfi/p2p"
)

var (
	_ validators.Connector = (*Network)(nil)
	_ NodeSampler          = (*PeerSampler)(nil)

	opLabel      = "op"
	handlerLabel = "handlerID"
	labelNames   = []string{opLabel, handlerLabel}
)

// ClientOption configures Client
type ClientOption interface {
	apply(options *clientOptions)
}

type clientOptionFunc func(options *clientOptions)

func (o clientOptionFunc) apply(options *clientOptions) {
	o(options)
}

// WithValidatorSampling configures Client.AppRequestAny to sample validators
func WithValidatorSampling(validators *Validators) ClientOption {
	return clientOptionFunc(func(options *clientOptions) {
		options.nodeSampler = validators
	})
}

// clientOptions holds client-configurable values
type clientOptions struct {
	// nodeSampler is used to select nodes to route Client.AppRequestAny to
	nodeSampler NodeSampler
}

// NewNetwork returns an instance of Network
func NewNetwork(
	log log.Logger,
	sender p2p.Sender,
	registerer metric.Registerer,
	namespace string,
) (*Network, error) {
	m := metrics{
		msgTime: metric.NewGaugeVec(
			metric.GaugeOpts{
				Namespace: namespace,
				Name:      "msg_time",
				Help:      "message handling time (ns)",
			},
			labelNames,
		),
		msgCount: metric.NewCounterVec(
			metric.CounterOpts{
				Namespace: namespace,
				Name:      "msg_count",
				Help:      "message count (n)",
			},
			labelNames,
		),
	}

	err := errors.Join(
	)
	if err != nil {
		return nil, err
	}

	return &Network{
		Peers: &Peers{
			set: set.NewSampleableSet[ids.NodeID](0),
		},
		log:    log,
		sender: sender,
		router: newRouter(log, sender, m),
	}, nil
}

// Network exposes networking state and supports building p2p application
// protocols
type Network struct {
	Peers *Peers

	log    log.Logger
	sender p2p.Sender

	router *router
}

// Request handles an incoming request
func (n *Network) Request(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) ([]byte, *p2p.Error) {
	return n.router.Request(ctx, nodeID, requestID, deadline, msg)
}

// Response handles an incoming response
func (n *Network) Response(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	return n.router.Response(ctx, nodeID, requestID, msg)
}

// Gossip handles an incoming gossip message
func (n *Network) Gossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return n.router.Gossip(ctx, nodeID, msg)
}

// RequestFailed handles a failed request
func (n *Network) RequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, err *p2p.Error) error {
	return n.router.RequestFailed(ctx, nodeID, requestID, err)
}

func (n *Network) Connected(_ context.Context, nodeID ids.NodeID, _ *consensusversion.Application) error {
	n.Peers.add(nodeID)
	return nil
}

func (n *Network) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	n.Peers.remove(nodeID)
	return nil
}

// NewClient returns a Client that can be used to send messages for the
// corresponding protocol.
func (n *Network) NewClient(handlerID uint64, options ...ClientOption) *Client {
	client := &Client{
		handlerIDStr:  strconv.FormatUint(handlerID, 10),
		handlerPrefix: ProtocolPrefix(handlerID),
		sender:        n.sender,
		router:        n.router,
		options: &clientOptions{
			nodeSampler: &PeerSampler{
				Peers: n.Peers,
			},
		},
	}

	for _, option := range options {
		option.apply(client.options)
	}

	return client
}

// AddHandler reserves an identifier for an application protocol
func (n *Network) AddHandler(handlerID uint64, handler Handler) error {
	return n.router.addHandler(handlerID, handler)
}

// Peers contains metadata about the current set of connected peers
type Peers struct {
	lock sync.RWMutex
	set  set.SampleableSet[ids.NodeID]
}

func (p *Peers) add(nodeID ids.NodeID) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.set.Add(nodeID)
}

func (p *Peers) remove(nodeID ids.NodeID) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.set.Remove(nodeID)
}

func (p *Peers) has(nodeID ids.NodeID) bool {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.set.Contains(nodeID)
}

// Sample returns a pseudo-random sample of up to limit Peers
func (p *Peers) Sample(limit int) []ids.NodeID {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.set.Sample(limit)
}

// PeerSampler implements NodeSampler
type PeerSampler struct {
	Peers *Peers
}

func (p PeerSampler) Sample(_ context.Context, limit int) []ids.NodeID {
	return p.Peers.Sample(limit)
}

func ProtocolPrefix(handlerID uint64) []byte {
	return binary.AppendUvarint(nil, handlerID)
}
