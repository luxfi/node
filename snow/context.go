// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/node/api/keystore"
	"github.com/luxfi/node/api/metrics"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/validators"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/crypto/bls"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/vms/platformvm/warp"
)

// ContextInitializable represents an object that can be initialized
// given a *Context object
type ContextInitializable interface {
	// InitCtx initializes an object provided a *Context object
	InitCtx(ctx *Context)
}

// Context is information about the current execution.
// [NetworkID] is the ID of the network this context exists within.
// [ChainID] is the ID of the chain this context exists within.
// [NodeID] is the ID of this node
type Context struct {
	NetworkID uint32
	SubnetID  ids.ID
	ChainID   ids.ID
	NodeID    ids.NodeID
	PublicKey *bls.PublicKey

	XChainID    ids.ID
	CChainID    ids.ID
	LUXAssetID ids.ID

	Log          logging.Logger
	Lock         sync.RWMutex
	Keystore     keystore.BlockchainKeystore
	SharedMemory atomic.SharedMemory
	BCLookup     ids.AliaserReader
	Metrics      metrics.OptionalGatherer

	WarpSigner warp.Signer

	// snowman++ attributes
	ValidatorState validators.State // interface for P-Chain validators
	// Chain-specific directory where arbitrary data can be written
	ChainDataDir string
}

// Expose gatherer interface for unit testing.
type Registerer interface {
	prometheus.Registerer
	prometheus.Gatherer
}

type ConsensusContext struct {
	*Context

	// Registers all common and snowman consensus metrics. Unlike the lux
	// consensus engine metrics, we do not prefix the name with the engine name,
	// as snowman is used for all chains by default.
	Registerer Registerer
	// Only used to register Lux consensus metrics. Previously, all
	// metrics were prefixed with "lux_{chainID}_". Now we add lux
	// to the prefix, "lux_{chainID}_lux_", to differentiate
	// consensus operations after the DAG linearization.
	LuxRegisterer Registerer

	// BlockAcceptor is the callback that will be fired whenever a VM is
	// notified that their block was accepted.
	BlockAcceptor Acceptor

	// TxAcceptor is the callback that will be fired whenever a VM is notified
	// that their transaction was accepted.
	TxAcceptor Acceptor

	// VertexAcceptor is the callback that will be fired whenever a vertex was
	// accepted.
	VertexAcceptor Acceptor

	// State indicates the current state of this consensus instance.
	State utils.Atomic[EngineState]

	// True iff this chain is executing transactions as part of bootstrapping.
	Executing utils.Atomic[bool]

	// True iff this chain is currently state-syncing
	StateSyncing utils.Atomic[bool]
}

func DefaultContextTest() *Context {
	sk, err := bls.NewSecretKey()
	if err != nil {
		panic(err)
	}
	pk := bls.PublicFromSecretKey(sk)
	return &Context{
		NetworkID:    0,
		SubnetID:     ids.Empty,
		ChainID:      ids.Empty,
		NodeID:       ids.EmptyNodeID,
		PublicKey:    pk,
		Log:          logging.NoLog{},
		BCLookup:     ids.NewAliaser(),
		Metrics:      metrics.NewOptionalGatherer(),
		ChainDataDir: "",
	}
}

func DefaultConsensusContextTest() *ConsensusContext {
	return &ConsensusContext{
		Context:             DefaultContextTest(),
		Registerer:          prometheus.NewRegistry(),
		LuxRegisterer: prometheus.NewRegistry(),
		BlockAcceptor:       noOpAcceptor{},
		TxAcceptor:          noOpAcceptor{},
		VertexAcceptor:      noOpAcceptor{},
	}
}
