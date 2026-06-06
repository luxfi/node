// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"

	"github.com/luxfi/node/vms/pcodecs"
)

const (
	CodecVersion0Tag        = "v0"
	CodecVersion0    uint16 = 0

	CodecVersion1Tag        = "v1"
	CodecVersion1    uint16 = 1
)

var MetadataCodec pcodecs.Manager

func init() {
	c0 := pcodecs.NewLinearCodec()
	c1 := pcodecs.NewLinearCodec()
	MetadataCodec = pcodecs.NewMaxInt32Manager()

	err := errors.Join(
		MetadataCodec.RegisterCodec(CodecVersion0, c0),
		MetadataCodec.RegisterCodec(CodecVersion1, c1),
	)
	if err != nil {
		panic(err)
	}
}
