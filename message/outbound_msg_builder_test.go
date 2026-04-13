// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	compression "github.com/luxfi/compress"
	"github.com/luxfi/ids"
	"github.com/luxfi/metric"
)

func Test_newOutboundBuilder(t *testing.T) {
	t.Parallel()

	mb, err := newMsgBuilder(
		metric.NewRegistry(),
		10*time.Second,
	)
	require.NoError(t, err)

	for _, compressionType := range []compression.Type{
		compression.TypeNone,
		compression.TypeZstd,
	} {
		t.Run(compressionType.String(), func(t *testing.T) {
			builder := newOutboundBuilder(compressionType, mb)

			outMsg, err := builder.GetAcceptedStateSummary(
				ids.GenerateTestID(),
				12345,
				time.Hour,
				[]uint64{1000, 2000},
			)
			require.NoError(t, err)
			t.Logf("outbound message with compression type %s built message with size %d", compressionType, len(outMsg.Bytes()))
		})
	}
}
