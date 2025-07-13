// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package yvm

import (
	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/codec/linearcodec"
)

const CodecVersion = 0

var (
	Codec codec.Manager
)

func init() {
	c := linearcodec.NewDefault()
	Codec = codec.NewDefaultManager()
	if err := Codec.RegisterCodec(CodecVersion, c); err != nil {
		panic(err)
	}
}

// codecVersion is the current codec version
const (
	codecVersion = CodecVersion
	maxRootSize  = 32 // SHA-256 hash
	maxChains    = 16
	maxBlockSize = 5 * 1024
)