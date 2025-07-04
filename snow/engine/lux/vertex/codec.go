// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/codec/linearcodec"
	"github.com/luxfi/node/codec/reflectcodec"
	"github.com/luxfi/node/utils/units"
)

const (
	// maxSize is the maximum allowed vertex size. It is necessary to deter DoS
	maxSize = units.MiB

	codecVersion            uint16 = 0
	codecVersionWithStopVtx uint16 = 1
)

var c codec.Manager

func init() {
	lc := linearcodec.New([]string{reflectcodec.DefaultTagName + "V0"})
	lc2 := linearcodec.New([]string{reflectcodec.DefaultTagName + "V1"})

	c = codec.NewManager(maxSize)
	// for backward compatibility, still register the initial codec version
	if err := c.RegisterCodec(codecVersion, lc); err != nil {
		panic(err)
	}
	if err := c.RegisterCodec(codecVersionWithStopVtx, lc2); err != nil {
		panic(err)
	}
}
