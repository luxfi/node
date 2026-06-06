// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"errors"

	"github.com/luxfi/node/vms/pcodecs"
)

const CodecVersion = 0

var Codec pcodecs.Manager

func init() {
	c := pcodecs.NewLinearCodec()
	Codec = pcodecs.NewMaxInt32Manager()

	err := errors.Join(
		c.RegisterType(&Transfer{}),
		c.RegisterType(&Export{}),
		c.RegisterType(&Import{}),
		Codec.RegisterCodec(CodecVersion, c),
	)
	if err != nil {
		panic(err)
	}
}
