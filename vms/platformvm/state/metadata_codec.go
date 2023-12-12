// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"math"

	"github.com/luxdefi/node/codec"
	"github.com/luxdefi/node/codec/linearcodec"
)

const (
	v0tag = "v0"
	v0    = uint16(0)
)

var metadataCodec codec.Manager

func init() {
	c := linearcodec.New([]string{v0tag}, math.MaxInt32)
	metadataCodec = codec.NewManager(math.MaxInt32)

	err := metadataCodec.RegisterCodec(v0, c)
	if err != nil {
		panic(err)
	}
}
