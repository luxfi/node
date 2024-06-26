// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"math"

	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/codec/linearcodec"
	"github.com/luxfi/node/utils"
)

const codecVersion = 0

// Codec does serialization and deserialization for Warp messages.
var c codec.Manager

func init() {
	c = codec.NewManager(math.MaxInt)
	lc := linearcodec.NewCustomMaxLength(math.MaxInt32)

	err := utils.Err(
		lc.RegisterType(&BitSetSignature{}),
		c.RegisterCodec(codecVersion, lc),
	)
	if err != nil {
		panic(err)
	}
}
