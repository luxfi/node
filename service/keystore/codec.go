// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"github.com/luxfi/codec"
	"github.com/luxfi/codec/linearcodec"
)

const (
	CodecVersion = 0

	// maxPackerSize caps the size of any single keystore codec payload.
	// Real user keystores are well under 1 MiB; the previous 1 GiB ceiling
	// was an OOM vector for authenticated RPC callers. 16 MiB is generous
	// for any plausible future wallet/seed container while bounding
	// server-side allocation to a non-pathological size.
	// See papers/oom-audit-2026-04-12.tex F-4.
	maxPackerSize = 16 * 1024 * 1024 // 16 MiB
)

var Codec codec.Manager

func init() {
	lc := linearcodec.NewDefault()
	Codec = codec.NewManager(maxPackerSize)
	if err := Codec.RegisterCodec(CodecVersion, lc); err != nil {
		panic(err)
	}
}
