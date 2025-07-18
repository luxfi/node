// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"math"

	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/codec/linearcodec"
)

const CodecVersion = 0

var Codec codec.Manager

func init() {
	lc := linearcodec.NewDefault()
	Codec = codec.NewManager(math.MaxInt)

	if err := Codec.RegisterCodec(CodecVersion, lc); err != nil {
		panic(err)
	}
}
