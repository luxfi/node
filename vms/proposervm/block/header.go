// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import "github.com/luxfi/ids"

type Header interface {
	ChainID() ids.ID
	ParentID() ids.ID
	BodyID() ids.ID
	Bytes() []byte
}

// statelessHeader is the (chain, parent, body) binding the proposer signs. Its
// bytes are a single fixed-shape zap object (see buildHeaderBuffer); the struct
// caches the three IDs plus the encoded bytes.
type statelessHeader struct {
	Chain  ids.ID
	Parent ids.ID
	Body   ids.ID

	bytes []byte
}

func (h *statelessHeader) ChainID() ids.ID {
	return h.Chain
}

func (h *statelessHeader) ParentID() ids.ID {
	return h.Parent
}

func (h *statelessHeader) BodyID() ids.ID {
	return h.Body
}

func (h *statelessHeader) Bytes() []byte {
	return h.bytes
}
