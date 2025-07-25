// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/consensus/engine/core"
	"github.com/luxfi/node/subnets"
	"github.com/luxfi/node/utils/set"
)

// ExternalSender sends consensus messages to other validators
// Right now this is implemented in the networking package
type ExternalSender interface {
	Send(
		msg message.OutboundMessage,
		config core.SendConfig,
		subnetID ids.ID,
		allower subnets.Allower,
	) set.Set[ids.NodeID]
}
