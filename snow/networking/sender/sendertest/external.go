// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sendertest

import (
	"errors"
	"testing"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/snow/engine/common"
	"github.com/luxfi/node/snow/networking/sender"
	"github.com/luxfi/node/subnets"
	"github.com/luxfi/node/utils/set"
)

var (
	_ sender.ExternalSender = (*External)(nil)

	errSend = errors.New("unexpectedly called Send")
)

// External is a test sender
type External struct {
	TB testing.TB

	CantSend bool

	SendF func(msg message.OutboundMessage, config common.SendConfig, subnetID ids.ID, allower subnets.Allower) set.Set[ids.NodeID]
}

// Default set the default callable value to [cant]
func (s *External) Default(cant bool) {
	s.CantSend = cant
}

func (s *External) Send(
	msg message.OutboundMessage,
	config common.SendConfig,
	subnetID ids.ID,
	allower subnets.Allower,
) set.Set[ids.NodeID] {
	if s.SendF != nil {
		return s.SendF(msg, config, subnetID, allower)
	}
	if s.CantSend {
		if s.TB != nil {
			s.TB.Helper()
			s.TB.Fatal(errSend)
		}
	}
	return nil
}
