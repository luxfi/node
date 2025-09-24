// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"time"

	"github.com/luxfi/consensus/networking/tracker"
	"github.com/luxfi/consensus/uptime"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network/throttling"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/version"
)

// InboundHandler handles inbound messages
type InboundHandler interface {
	HandleInbound(ctx context.Context, msg message.InboundMessage)
}

// InboundHandlerFunc is a function adapter for InboundHandler
type InboundHandlerFunc func(context.Context, message.InboundMessage)

func (f InboundHandlerFunc) HandleInbound(ctx context.Context, msg message.InboundMessage) {
	f(ctx, msg)
}

type Config struct {
	// Size, in bytes, of the buffer this peer reads messages into
	ReadBufferSize int
	// Size, in bytes, of the buffer this peer writes messages into
	WriteBufferSize int
	Clock           mockable.Clock
	Metrics         *Metrics
	MessageCreator  message.Creator

	Log                  log.Logger
	InboundMsgThrottler  throttling.InboundMsgThrottler
	Network              Network
	Router               InboundHandler
	VersionCompatibility version.Compatibility
	// MySubnets does not include the primary network ID
	MySubnets          set.Set[ids.ID]
	Beacons            validators.Manager
	Validators         validators.Manager
	NetworkID          uint32
	PingFrequency      time.Duration
	PongTimeout        time.Duration
	MaxClockDifference time.Duration

	SupportedLPs []uint32
	ObjectedLPs  []uint32

	// Unix time of the last message sent and received respectively
	// Must only be accessed atomically
	LastSent, LastReceived int64

	// Tracks CPU/disk usage caused by each peer.
	ResourceTracker tracker.ResourceTracker

	// Calculates uptime of peers
	UptimeCalculator uptime.Calculator

	// Signs my IP so I can send my signed IP address in the Handshake message
	IPSigner *IPSigner
}
