// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"github.com/luxfi/node/v2/vms/platformvm/txs/fee"
)

// messageAdapter adapts a warp.Message to implement fee.WarpMessage
type messageAdapter struct {
	msg *Message
}

// GetSignature implements fee.WarpMessage
func (m *messageAdapter) GetSignature() fee.WarpSignature {
	return m.msg.Signature
}

// parseMessageForFee adapts ParseMessage to return fee.WarpMessage
func parseMessageForFee(bytes []byte) (fee.WarpMessage, error) {
	msg, err := ParseMessage(bytes)
	if err != nil {
		return nil, err
	}
	return &messageAdapter{msg: msg}, nil
}

// init registers the warp message parser with the fee package
func init() {
	fee.SetWarpMessageParser(parseMessageForFee)
}