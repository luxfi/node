// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

var _ Payload = (*Hash)(nil)

// Hash wire: pkind u8 @0, Hash 32B @1 = 33 bytes.
const (
	hashOffKind = offPKind
	hashOffHash = 1
	hashSize    = 33
)

type Hash struct {
	Hash ids.ID `json:"hash"`

	bytes []byte
}

// NewHash creates a new *Hash and initializes it.
func NewHash(hash ids.ID) (*Hash, error) {
	bhp := &Hash{
		Hash: hash,
	}
	b := zap.NewBuilder(zap.HeaderSize + hashSize)
	ob := b.StartObject(hashSize)
	ob.SetUint8(hashOffKind, uint8(pkindHash))
	ob.SetBytesFixed(hashOffHash, hash[:])
	ob.FinishAsRoot()
	bhp.initialize(b.Finish())
	return bhp, nil
}

// ParseHash converts a slice of bytes into an initialized Hash.
func ParseHash(b []byte) (*Hash, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*Hash)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}

// Bytes returns the binary representation of this payload. It assumes that the
// payload is initialized from either NewHash or Parse.
func (b *Hash) Bytes() []byte {
	return b.bytes
}

func (b *Hash) initialize(bytes []byte) {
	b.bytes = bytes
}
