// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxdefi/luxd/utils/constants"
)

var _ Creator = (*creator)(nil)

type Creator interface {
	OutboundMsgBuilder
	InboundMsgBuilder
	InternalMsgBuilder
}

type creator struct {
	OutboundMsgBuilder
	InboundMsgBuilder
	InternalMsgBuilder
}

func NewCreator(metrics prometheus.Registerer, parentNamespace string, compressionEnabled bool, maxInboundMessageTimeout time.Duration) (Creator, error) {
	namespace := fmt.Sprintf("%s_codec", parentNamespace)
	builder, err := newMsgBuilder(namespace, metrics, int64(constants.DefaultMaxMessageSize), maxInboundMessageTimeout)
	if err != nil {
		return nil, err
	}
	return &creator{
		OutboundMsgBuilder: newOutboundBuilder(compressionEnabled, builder),
		InboundMsgBuilder:  newInboundBuilder(builder),
		InternalMsgBuilder: NewInternalBuilder(),
	}, nil
}
