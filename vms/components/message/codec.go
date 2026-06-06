// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/luxfi/constants"
	"github.com/luxfi/node/vms/pcodecs"
	"github.com/luxfi/utils"
)

const (
	codecVersion   = 0
	maxMessageSize = 512 * constants.KiB
	maxSliceLen    = maxMessageSize
)

// Codec does serialization and deserialization
var c pcodecs.Manager

func init() {
	c = pcodecs.NewManager(maxMessageSize)
	lc := pcodecs.NewLinearCodec()

	err := utils.Err(
		lc.RegisterType(&Tx{}),
		c.RegisterCodec(codecVersion, lc),
	)
	if err != nil {
		panic(err)
	}
}
