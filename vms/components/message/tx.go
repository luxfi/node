// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

var _ Message = (*Tx)(nil)

type Tx struct {
	message

	Tx []byte
}

func (msg *Tx) Handle(handler Handler, nodeID ids.NodeID, requestID uint32) error {
	return handler.HandleTx(nodeID, requestID, msg)
}

// marshal writes the Tx message as one native ZAP object: kind byte at
// offset 0, gossiped tx bytes at offset 1.
func (msg *Tx) marshal() ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + sizeMsgTx + len(msg.Tx))
	ob := b.StartObject(sizeMsgTx)
	ob.SetUint8(offMsgKind, uint8(msgKindTx))
	ob.SetBytes(offMsgTx, msg.Tx)
	ob.FinishAsRoot()
	return b.Finish(), nil
}
