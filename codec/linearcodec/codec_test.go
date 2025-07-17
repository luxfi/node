// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linearcodec

import (
	"testing"

	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/codec/codectest"
)

func TestVectors(t *testing.T) {
	codectest.RunAll(t, func() codec.GeneralCodec {
		return NewDefault()
	})
}

func TestMultipleTags(t *testing.T) {
	codectest.RunAllMultipleTags(t, func() codec.GeneralCodec {
		return New([]string{"tag1", "tag2"})
	})
}

func FuzzStructUnmarshalLinearCodec(f *testing.F) {
	c := NewDefault()
	codectest.FuzzStructUnmarshal(c, f)
}
