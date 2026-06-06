// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"

	"github.com/luxfi/node/vms/pcodecs"
)

const CodecVersion = 0

var Codec pcodecs.Manager

func init() {
	Codec = pcodecs.NewMaxIntManager()
	lc := pcodecs.NewLinearCodec()

	err := errors.Join(
		lc.RegisterType(&ChainToL1Conversion{}),
		lc.RegisterType(&RegisterL1Validator{}),
		lc.RegisterType(&L1ValidatorRegistration{}),
		lc.RegisterType(&L1ValidatorWeight{}),
		Codec.RegisterCodec(CodecVersion, lc),
	)
	if err != nil {
		panic(err)
	}
}
