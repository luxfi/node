// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"time"

<<<<<<< HEAD
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/message"
	"github.com/luxdefi/luxd/network/throttling"
	"github.com/luxdefi/luxd/snow/networking/router"
	"github.com/luxdefi/luxd/snow/networking/tracker"
	"github.com/luxdefi/luxd/snow/validators"
	"github.com/luxdefi/luxd/utils/logging"
	"github.com/luxdefi/luxd/utils/timer/mockable"
	"github.com/luxdefi/luxd/version"
=======
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
>>>>>>> 624d97c11 (Gossip subnet uptimes (#2319))
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
	Beacons              validators.Set
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
