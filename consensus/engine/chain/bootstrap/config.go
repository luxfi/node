// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"github.com/luxfi/node/database"
	"github.com/luxfi/node/network/p2p"
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/engine"
	"github.com/luxfi/node/consensus/engine/tracker"
	"github.com/luxfi/node/consensus/engine/chain/block"
	"github.com/luxfi/node/consensus/validators"
)

type Config struct {
	engine.AllGetsServer

	Ctx     *consensus.Context
	Beacons validators.Manager

	SampleK          int
	StartupTracker   tracker.Startup
	Sender           engine.Sender
	BootstrapTracker engine.BootstrapTracker

	// PeerTracker manages the set of nodes that we fetch the next block from.
	PeerTracker *p2p.PeerTracker

	// This node will only consider the first [AncestorsMaxContainersReceived]
	// containers in an ancestors message it receives.
	AncestorsMaxContainersReceived int

	// Database used to track the fetched, but not yet executed, blocks during
	// bootstrapping.
	DB database.Database

	VM block.ChainVM

	// NonVerifyingParse parses blocks without verifying them.
	NonVerifyingParse block.ParseFunc

	Bootstrapped func()

	engine.Haltable
}
