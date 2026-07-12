// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"fmt"

	"github.com/luxfi/zap"
)

// Message defines the standard format for a Warp message.
//
// Wire (native ZAP): one object holding the self-delimiting unsigned-message
// buffer and the wkind-tagged signature buffer as two opaque byte fields —
// unsigned bytes @0, signature bytes @8 = 16-byte object header. Message is
// the container shape, so it needs no kind of its own; the signature it
// carries is dispatched by ITS wkind at offset 0.
type Message struct {
	UnsignedMessage
	Signature Signature

	bytes []byte
}

const (
	msgOffUnsigned  = 0
	msgOffSignature = 8
	msgSize         = 16
)

// NewMessage creates a new *Message and initializes it.
func NewMessage(
	unsignedMsg *UnsignedMessage,
	signature Signature,
) (*Message, error) {
	msg := &Message{
		UnsignedMessage: *unsignedMsg,
		Signature:       signature,
	}
	return msg, msg.Initialize()
}

// ParseMessage converts a slice of bytes into an initialized *Message.
func ParseMessage(b []byte) (*Message, error) {
	zm, err := zap.Parse(b)
	if err != nil {
		return nil, err
	}
	root := zm.Root()

	unsignedMsg, err := ParseUnsignedMessage(append([]byte(nil), root.Bytes(msgOffUnsigned)...))
	if err != nil {
		return nil, fmt.Errorf("failed to parse unsigned message: %w", err)
	}
	sig, err := parseSignature(append([]byte(nil), root.Bytes(msgOffSignature)...))
	if err != nil {
		return nil, fmt.Errorf("failed to parse signature: %w", err)
	}
	return &Message{
		UnsignedMessage: *unsignedMsg,
		Signature:       sig,
		bytes:           b,
	}, nil
}

// Initialize recalculates the result of Bytes(). It does not call Initialize()
// on the UnsignedMessage.
func (m *Message) Initialize() error {
	sigBytes, err := marshalSignature(m.Signature)
	if err != nil {
		return err
	}
	unsignedBytes := m.UnsignedMessage.Bytes()
	b := zap.NewBuilder(zap.HeaderSize + msgSize + len(unsignedBytes) + len(sigBytes) + 32)
	ob := b.StartObject(msgSize)
	ob.SetBytes(msgOffUnsigned, unsignedBytes)
	ob.SetBytes(msgOffSignature, sigBytes)
	ob.FinishAsRoot()
	m.bytes = b.Finish()
	return nil
}

// Bytes returns the binary representation of this message. It assumes that the
// message is initialized from either New, Parse, or an explicit call to
// Initialize.
func (m *Message) Bytes() []byte {
	return m.bytes
}

func (m *Message) String() string {
	return fmt.Sprintf("WarpMessage(%s, %s)", &m.UnsignedMessage, m.Signature)
}
