// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"time"

	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/message"
	"github.com/luxdefi/node/network/throttling"
	"github.com/luxdefi/node/snow/networking/router"
	"github.com/luxdefi/node/snow/networking/tracker"
	"github.com/luxdefi/node/snow/uptime"
	"github.com/luxdefi/node/snow/validators"
	"github.com/luxdefi/node/utils/logging"
	"github.com/luxdefi/node/utils/set"
	"github.com/luxdefi/node/utils/timer/mockable"
	"github.com/luxdefi/node/version"
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
