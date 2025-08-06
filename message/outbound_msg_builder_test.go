// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"
	"time"

	"github.com/luxfi/metrics"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/compression"
	"github.com/luxfi/log"
)

func Test_newOutboundBuilder(t *testing.T) {
	t.Parallel()

	mb, err := newMsgBuilder(
		nil,
		metrics.NewNoOpMetrics("test"),
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
