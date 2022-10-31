// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/utils/constants"
)

func Test_newInboundBuilder(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	mb, err := newMsgBuilder("test", prometheus.NewRegistry(), int64(constants.DefaultMaxMessageSize), 5*time.Second)
	require.NoError(err)

	builder := newInboundBuilder(mb)

	inMsg := builder.InboundAccepted(
		ids.GenerateTestID(),
		uint32(12345),
		[]ids.ID{ids.GenerateTestID()},
		ids.GenerateTestNodeID(),
	)

	t.Logf("outbound message built %q", inMsg.Op().String())
}
