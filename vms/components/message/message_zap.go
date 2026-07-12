// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

// Native ZAP wire for gossip messages: the struct IS the wire. Each message
// is one zap object keyed by a 1-byte kind discriminator at object offset 0 —
// the whole dispatch. Parse reads it and returns the typed message. There is
// no codec, no version prefix, no slot map.
//
// Object fixed section (offsets object-relative, little-endian):
//
//	kind u8    @ 0   tx=1
//	Tx   bytes @ 1   ptr to the gossiped tx bytes (Tx only)

import (
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

var (
	_ Message = (*Tx)(nil)

	// ErrUnknownMessageKind is returned when the 1-byte kind discriminator at
	// object offset 0 does not correspond to a known message type.
	ErrUnknownMessageKind = errors.New("unknown message kind")
)

// msgKind is the 1-byte discriminator at object offset 0 of every message
// buffer. Parse reads it and dispatches to the typed message.
type msgKind uint8

const (
	msgKindReserved msgKind = iota
	msgKindTx
)

// Fixed wire offsets (object-relative) and object size for a Tx message.
const (
	offMsgKind = 0
	offMsgTx   = 1
	sizeMsgTx  = 9 // kind(1) + bytes ptr(8)
)

type Message interface {
	// Handle this message with the correct message handler
	Handle(handler Handler, nodeID ids.NodeID, requestID uint32) error

	// initialize should be called whenever a message is built or parsed
	initialize([]byte)

	// Bytes returns the binary representation of this message
	//
	// Bytes should only be called after being initialized
	Bytes() []byte

	// marshal encodes the message to its native ZAP wire form (kind byte at
	// object offset 0). Each concrete type writes its own kind — this is the
	// whole dispatch, no codec.
	marshal() ([]byte, error)
}

type message []byte

func (m *message) initialize(bytes []byte) {
	*m = bytes
}

func (m *message) Bytes() []byte {
	return *m
}

func Parse(bytes []byte) (Message, error) {
	zmsg, err := zap.Parse(bytes)
	if err != nil {
		return nil, err
	}
	obj := zmsg.Root()
	switch msgKind(obj.Uint8(offMsgKind)) {
	case msgKindTx:
		msg := &Tx{Tx: append([]byte(nil), obj.Bytes(offMsgTx)...)}
		msg.initialize(bytes)
		return msg, nil
	default:
		return nil, ErrUnknownMessageKind
	}
}

func Build(msg Message) ([]byte, error) {
	bytes, err := msg.marshal()
	if err != nil {
		return nil, err
	}
	msg.initialize(bytes)
	return bytes, nil
}
