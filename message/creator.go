// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/node/utils/compression"
	"github.com/luxfi/node/utils/logging"
)

var _ Creator = (*creator)(nil)

type Creator interface {
	OutboundMsgBuilder
	InboundMsgBuilder
}

type creator struct {
	OutboundMsgBuilder
	InboundMsgBuilder
}

func NewCreator(
	log logging.Logger,
	metrics prometheus.Registerer,
	compressionType compression.Type,
	maxMessageTimeout time.Duration,
) (Creator, error) {
	builder, err := newMsgBuilder(
		log,
		metrics,
		maxMessageTimeout,
	)
	if err != nil {
		return nil, err
	}

	return &creator{
		OutboundMsgBuilder: newOutboundBuilder(compressionType, builder),
		InboundMsgBuilder:  newInboundBuilder(builder),
	}, nil
}
