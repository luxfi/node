// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bft

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/networking/sender"
	"github.com/luxfi/node/consensus/validators"
	"github.com/luxfi/node/message"
	luxlog "github.com/luxfi/log"
)

// Config wraps all the parameters needed for a simplex engine
type Config struct {
	Ctx SimplexChainContext
	Log luxlog.Logger

	Sender             sender.ExternalSender
	OutboundMsgBuilder message.OutboundMsgBuilder

	// Validators is a map of node IDs to their validator information.
	// This tells the node about the current membership set, and should be consistent
	// across all nodes in the subnet.
	Validators map[ids.NodeID]*validators.GetValidatorOutput

	// SignBLS is the signing function used for this node to sign messages.
	SignBLS SignFunc
}

// Context is information about the current execution.
type SimplexChainContext struct {
	// Network is the ID of the network this context exists within.
	NodeID ids.NodeID

	// ChainID is the ID of the chain this context exists within.
	ChainID ids.ID

	// SubnetID is the ID of the subnet this context exists within.
	SubnetID ids.ID

	// NodeID is the ID of this node
	NetworkID uint32
}
