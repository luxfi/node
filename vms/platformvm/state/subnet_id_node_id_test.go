// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thepudds/fzgen/fuzzer"
)

func FuzzNetIDNodeIDMarshal(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		require := require.New(t)

		var v netIDNodeID
		fz := fuzzer.NewFuzzer(data)
		fz.Fill(&v)

		marshalledData := v.Marshal()

		var parsed netIDNodeID
		require.NoError(parsed.Unmarshal(marshalledData))
		require.Equal(v, parsed)
	})
}

func FuzzNetIDNodeIDUnmarshal(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		require := require.New(t)

		var v netIDNodeID
		if err := v.Unmarshal(data); err != nil {
			require.ErrorIs(err, errUnexpectedNetIDNodeIDLength)
			return
		}

		marshalledData := v.Marshal()
		require.Equal(data, marshalledData)
	})
}

func FuzzNetIDNodeIDOrdering(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		var (
			v0 netIDNodeID
			v1 netIDNodeID
		)
		fz := fuzzer.NewFuzzer(data)
		fz.Fill(&v0, &v1)

		if v0.netID == v1.netID {
			return
		}

		key0 := v0.Marshal()
		key1 := v1.Marshal()
		require.Equal(
			t,
			v0.netID.Compare(v1.netID),
			bytes.Compare(key0, key1),
		)
	})
}
