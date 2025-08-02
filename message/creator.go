// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/node/v2/utils/compression"
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
	metrics prometheus.Registerer,
	compressionType compression.Type,
	maxMessageTimeout time.Duration,
) (Creator, error) {
	builder, err := newMsgBuilder(
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
