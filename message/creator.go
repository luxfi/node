// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"time"

	"github.com/luxfi/log"
	metric "github.com/luxfi/metric"
	"github.com/luxfi/node/utils/compression"
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
	log log.Logger,
	m metric.Metrics,
	compressionType compression.Type,
	maxMessageTimeout time.Duration,
) (Creator, error) {
	builder, err := newMsgBuilder(
		log,
		m,
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
