// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"fmt"
	"net/netip"
	"time"
	

	"github.com/luxfi/consensus/networking/router"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/utils/constants"
)

func ExampleStartTestPeer() {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	peerIP := netip.AddrPortFrom(
		netip.IPv6Loopback(),
		9631,
	)
	peer, err := StartTestPeer(
		ctx,
		peerIP,
		constants.LocalID,
		router.InboundHandlerFunc(func(_ context.Context, msgIntf interface{}) {
			if msg, ok := msgIntf.(message.InboundMessage); ok {
				fmt.Printf("handling %s\n", msg.Op())
			}
		}),
	)
	if err != nil {
		panic(err)
	}

	// Send messages here with [peer.Send].

	peer.StartClose()
	err = peer.AwaitClosed(ctx)
	if err != nil {
		panic(err)
	}
}
