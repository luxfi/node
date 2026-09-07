// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// option is zap-backed: msg is the single self-delimiting message with
// blkOption at offset 0. ID = hash(bytes); options carry no signature.
type option struct {
	msg   *zap.Message
	id    ids.ID
	bytes []byte
}

func (b *option) ID() ids.ID {
	return b.id
}

func (b *option) ParentID() ids.ID {
	return ids.ID(read32(b.msg.Root(), offOptParent))
}

func (b *option) Block() []byte {
	return b.msg.Root().Bytes(offOptInner)
}

func (b *option) Bytes() []byte {
	return b.bytes
}

func (b *option) initialize(bytes []byte) error {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return err
	}
	b.msg = msg
	b.bytes = bytes
	b.id = hash.ComputeHash256Array(bytes)
	return nil
}

func (*option) verify(ids.ID) error {
	return nil
}
