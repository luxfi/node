// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utils/hashing"
)

type option struct {
	PrntID     ids.ID `serialize:"true"`
	InnerBytes []byte `serialize:"true"`

	id    ids.ID
	bytes []byte
}

func (b *option) ID() ids.ID {
	return b.id
}

func (b *option) ParentID() ids.ID {
	return b.PrntID
}

func (b *option) Block() []byte {
	return b.InnerBytes
}

func (b *option) Bytes() []byte {
	return b.bytes
}

func (b *option) initialize(bytes []byte) error {
	b.id = hashing.ComputeHash256Array(bytes)
	b.bytes = bytes
	return nil
}

func (*option) verify(ids.ID) error {
	return nil
}
