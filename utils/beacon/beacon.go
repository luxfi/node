// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package beacon

import (
	"net/netip"

	"github.com/luxfi/node/ids"
)

var _ Beacon = (*beacon)(nil)

type Beacon interface {
	ID() ids.NodeID
	IP() netip.AddrPort
}

type beacon struct {
	id ids.NodeID
	ip netip.AddrPort
}

func New(id ids.NodeID, ip netip.AddrPort) Beacon {
	return &beacon{
		id: id,
		ip: ip,
	}
}

func (b *beacon) ID() ids.NodeID {
	return b.id
}

func (b *beacon) IP() netip.AddrPort {
	return b.ip
}
