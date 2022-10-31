// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"github.com/luxdefi/luxd/codec"
	"github.com/luxdefi/luxd/codec/linearcodec"
)

const codecVersion = 0

// codecManager is used to marshal and unmarshal dbElements and chain IDs.
var codecManager codec.Manager

func init() {
	linearCodec := linearcodec.NewDefault()
	codecManager = codec.NewDefaultManager()
	if err := codecManager.RegisterCodec(codecVersion, linearCodec); err != nil {
		panic(err)
	}
}
