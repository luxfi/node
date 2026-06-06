// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/luxfi/node/vms/pcodecs"
)

const CodecVersion = 0

var Codec pcodecs.Manager

func init() {
	lc := pcodecs.NewLinearCodec()
	Codec = pcodecs.NewMaxInt32Manager()

	err := Codec.RegisterCodec(CodecVersion, lc)
	if err != nil {
		panic(err)
	}
}
