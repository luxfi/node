// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

// Codec defines the wire format encoding interface for P2P messages.
// Implementations are selected via build tags (proto vs zap).
type Codec interface {
	Marshal(msg *P2PMessage) ([]byte, error)
	Unmarshal(data []byte, msg *P2PMessage) error
	Size(msg *P2PMessage) int
}

// P2PMessage is the top-level message container used by the codec.
// It wraps the inner message type for encoding/decoding.
type P2PMessage struct {
	inner interface{}
}

// SetInner sets the inner message
func (m *P2PMessage) SetInner(v interface{}) {
	m.inner = v
}

// Inner returns the inner message
func (m *P2PMessage) Inner() interface{} {
	return m.inner
}
