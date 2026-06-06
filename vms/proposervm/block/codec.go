// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"

	"github.com/luxfi/node/vms/pcodecs"
)

const CodecVersion = 0

var Codec pcodecs.Manager

func init() {
	lc := pcodecs.NewLinearCodec()
	// The maximum block size is enforced by the p2p message size limit.
	// See: [constants.DefaultMaxMessageSize]
	Codec = pcodecs.NewMaxIntManager()

	err := errors.Join(
		lc.RegisterType(&statelessBlock{}),
		lc.RegisterType(&option{}),
		Codec.RegisterCodec(CodecVersion, lc),
	)
	if err != nil {
		panic(err)
	}
}
