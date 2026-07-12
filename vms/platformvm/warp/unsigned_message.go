// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"fmt"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// UnsignedMessage defines the standard format for an unsigned Warp message.
//
// Wire (native ZAP, no kind byte — always parsed in context): NetworkID u32
// @0, SourceChainID 32B @4, Payload bytes @36 = 44-byte object header.
type UnsignedMessage struct {
	NetworkID     uint32 `json:"networkID"`
	SourceChainID ids.ID `json:"sourceChainID"`
	Payload       []byte `json:"payload"`

	bytes []byte
	id    ids.ID
}

const (
	umOffNetworkID = 0
	umOffSource    = 4
	umOffPayload   = 36
	umSize         = 44
)

// NewUnsignedMessage creates a new *UnsignedMessage and initializes it.
func NewUnsignedMessage(
	networkID uint32,
	sourceChainID ids.ID,
	payload []byte,
) (*UnsignedMessage, error) {
	msg := &UnsignedMessage{
		NetworkID:     networkID,
		SourceChainID: sourceChainID,
		Payload:       payload,
	}
	return msg, msg.Initialize()
}

// ParseUnsignedMessage converts a slice of bytes into an initialized
// *UnsignedMessage.
func ParseUnsignedMessage(b []byte) (*UnsignedMessage, error) {
	zm, err := zap.Parse(b)
	if err != nil {
		return nil, err
	}
	root := zm.Root()
	return &UnsignedMessage{
		NetworkID:     root.Uint32(umOffNetworkID),
		SourceChainID: readWireID(root, umOffSource),
		Payload:       append([]byte(nil), root.Bytes(umOffPayload)...),
		bytes:         b,
		id:            hash.ComputeHash256Array(b),
	}, nil
}

// Initialize recalculates the result of Bytes().
func (m *UnsignedMessage) Initialize() error {
	b := zap.NewBuilder(zap.HeaderSize + umSize + len(m.Payload) + 16)
	ob := b.StartObject(umSize)
	ob.SetUint32(umOffNetworkID, m.NetworkID)
	ob.SetBytesFixed(umOffSource, m.SourceChainID[:])
	ob.SetBytes(umOffPayload, m.Payload)
	ob.FinishAsRoot()
	m.bytes = b.Finish()
	m.id = hash.ComputeHash256Array(m.bytes)
	return nil
}

// Bytes returns the binary representation of this message. It assumes that the
// message is initialized from either New, Parse, or an explicit call to
// Initialize.
func (m *UnsignedMessage) Bytes() []byte {
	return m.bytes
}

// ID returns an identifier for this message. It assumes that the
// message is initialized from either New, Parse, or an explicit call to
// Initialize.
func (m *UnsignedMessage) ID() ids.ID {
	return m.id
}

func (m *UnsignedMessage) String() string {
	return fmt.Sprintf("UnsignedMessage(NetworkID = %d, SourceChainID = %s, Payload = %x)", m.NetworkID, m.SourceChainID, m.Payload)
}
