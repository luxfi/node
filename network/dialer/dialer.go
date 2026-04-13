// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dialer

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/node/network/throttling"
)

var _ Dialer = (*dialer)(nil)

// Dialer attempts to create a connection with the provided IP/port pair
type Dialer interface {
	// If [ctx] is canceled, gives up trying to connect to [ip]
	// and returns an error.
	Dial(ctx context.Context, ip netip.AddrPort) (net.Conn, error)
}

type dialer struct {
	dialer net.Dialer
	log    log.Logger
	network   string
	throttler throttling.DialThrottler
}

type Config struct {
	ThrottleRps       uint32        `json:"throttleRps"`
	ConnectionTimeout time.Duration `json:"connectionTimeout"`
}

// NewDialer returns a new Dialer that calls net.Dial with the provided network.
// [network] is the network passed into Dial. Should probably be "TCP".
// [dialerConfig.connectionTimeout] gives the timeout when dialing an IP.
// [dialerConfig.throttleRps] gives the max number of outgoing connection attempts/second.
// If [dialerConfig.throttleRps] == 0, outgoing connections aren't rate-limited.
func NewDialer(network string, dialerConfig Config, logger log.Logger) Dialer {
	var throttler throttling.DialThrottler
	if dialerConfig.ThrottleRps <= 0 {
		throttler = throttling.NewNoDialThrottler()
	} else {
		throttler = throttling.NewDialThrottler(int(dialerConfig.ThrottleRps))
	}
	logger.Debug(
		"creating dialer",
		log.Uint32("throttleRPS", dialerConfig.ThrottleRps),
		log.Duration("dialTimeout", dialerConfig.ConnectionTimeout),
	)
	return &dialer{
		dialer:    net.Dialer{Timeout: dialerConfig.ConnectionTimeout},
		log:       logger,
		network:   network,
		throttler: throttler,
	}
}

func (d *dialer) Dial(ctx context.Context, ip netip.AddrPort) (net.Conn, error) {
	d.log.Warn("[DIALER] >>> Dial ENTRY <<<",
		log.Stringer("ip", ip),
		log.String("network", d.network),
	)
	if err := d.throttler.Acquire(ctx); err != nil {
		d.log.Warn("[DIALER] throttler.Acquire FAILED",
			log.Stringer("ip", ip),
			"error", err,
		)
		return nil, err
	}
	d.log.Warn("[DIALER] throttler passed, calling net.Dial",
		log.Stringer("ip", ip),
	)
	conn, err := d.dialer.DialContext(ctx, d.network, ip.String())
	if err != nil {
		d.log.Warn("[DIALER] DialContext FAILED",
			log.Stringer("ip", ip),
			"error", err,
		)
		return nil, fmt.Errorf("error while dialing %s: %w", ip, err)
	}
	d.log.Warn("[DIALER] DialContext SUCCESS - TCP connection established",
		log.Stringer("ip", ip),
		log.String("localAddr", conn.LocalAddr().String()),
		log.String("remoteAddr", conn.RemoteAddr().String()),
	)
	return conn, nil
}
