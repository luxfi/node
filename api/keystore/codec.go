// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/codec/linearcodec"
	"github.com/luxfi/node/utils/units"
)

const (
	CodecVersion = 0

	maxPackerSize = 1 * units.GiB // max size, in bytes, of something being marshalled by Marshal()
)

var Codec codec.Manager

func init() {
	lc := linearcodec.NewDefault()
	Codec = codec.NewManager(maxPackerSize)
	if err := Codec.RegisterCodec(CodecVersion, lc); err != nil {
		panic(err)
	}
}
