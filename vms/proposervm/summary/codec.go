// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"errors"

	"github.com/luxfi/node/vms/pcodecs"
)

const CodecVersion = 0

var (
	Codec pcodecs.Manager

	errWrongCodecVersion = errors.New("wrong codec version")
)

func init() {
	lc := pcodecs.NewLinearCodec()
	Codec = pcodecs.NewMaxInt32Manager()
	if err := Codec.RegisterCodec(CodecVersion, lc); err != nil {
		panic(err)
	}
}
