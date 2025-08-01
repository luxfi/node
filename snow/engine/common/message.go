// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"github.com/luxfi/ids"
)

// Message represents a consensus message
type Message interface {
	// InboundMessage returns the inbound message
	InboundMessage() Message

	// OnFinalize is called when the message is finalized
	OnFinalize()

	// OnDrop is called when the message is dropped
	OnDrop()
}

// AppMessage represents an application message
type AppMessage interface {
	Message
}

// Fx represents an extension feature
type Fx struct{}

// AppSender sends application messages
type AppSender interface {
	// SendAppRequest sends an application request
	SendAppRequest(nodeID ids.NodeID, requestID uint32, msg []byte) error

	// SendAppResponse sends an application response
	SendAppResponse(nodeID ids.NodeID, requestID uint32, msg []byte) error

	// SendCrossChainAppRequest sends a cross-chain application request
	SendCrossChainAppRequest(chainID ids.ID, requestID uint32, msg []byte) error

	// SendCrossChainAppResponse sends a cross-chain application response
	SendCrossChainAppResponse(chainID ids.ID, requestID uint32, msg []byte) error
}