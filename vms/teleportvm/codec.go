// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleportvm

import (
	"errors"
	"math"

	"github.com/luxfi/codec"
	"github.com/luxfi/codec/linearcodec"
)

const codecVersion = 0

var Codec codec.Manager

func init() {
	Codec = codec.NewManager(math.MaxInt)
	lc := linearcodec.NewDefault()

	err := errors.Join(
		// Bridge types
		lc.RegisterType(&BridgeRequest{}),

		// Block type
		lc.RegisterType(&Block{}),

		// Genesis type
		lc.RegisterType(&Genesis{}),

		Codec.RegisterCodec(codecVersion, lc),
	)
	if err != nil {
		panic(err)
	}
}
