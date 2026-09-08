// Copyright (C) 2022-2026, Lux Industries Inc. All rights reserved.
// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"time"

	compression "github.com/luxfi/compress"
	"github.com/luxfi/metric"
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
	metrics metric.Registerer,
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
