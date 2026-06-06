// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"errors"

	"github.com/luxfi/node/vms/pcodecs"
)

const CodecVersion = 0

var (
	Codec       pcodecs.Manager
	LegacyCodec pcodecs.Manager
)

func init() {
	c := pcodecs.NewLinearCodec()
	Codec = pcodecs.NewDefaultManager()
	lc := pcodecs.NewLinearCodec()
	LegacyCodec = pcodecs.NewMaxInt32Manager()

	err := errors.Join(
		Codec.RegisterCodec(CodecVersion, c),
		LegacyCodec.RegisterCodec(CodecVersion, lc),
	)
	if err != nil {
		panic(err)
	}
}
