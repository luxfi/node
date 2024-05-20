// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"time"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network/throttling"
	"github.com/luxfi/node/snow/networking/router"
	"github.com/luxfi/node/snow/networking/tracker"
	"github.com/luxfi/node/snow/uptime"
	"github.com/luxfi/node/snow/validators"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/version"
)

type Config struct {
	// Size, in bytes, of the buffer this peer reads messages into
	ReadBufferSize int
	// Size, in bytes, of the buffer this peer writes messages into
	WriteBufferSize int
	Clock           mockable.Clock
	Metrics         *Metrics
	MessageCreator  message.Creator

	Log                  logging.Logger
	InboundMsgThrottler  throttling.InboundMsgThrottler
	Network              Network
	Router               router.InboundHandler
	VersionCompatibility version.Compatibility
	MySubnets            set.Set[ids.ID]
	Beacons              validators.Manager
	NetworkID            uint32
	PingFrequency        time.Duration
	PongTimeout          time.Duration
	MaxClockDifference   time.Duration

	// Unix time of the last message sent and received respectively
	// Must only be accessed atomically
	LastSent, LastReceived int64

	// Tracks CPU/disk usage caused by each peer.
	ResourceTracker tracker.ResourceTracker

	// Calculates uptime of peers
	UptimeCalculator uptime.Calculator

	// Signs my IP so I can send my signed IP address in the Version message
	IPSigner *IPSigner
}
