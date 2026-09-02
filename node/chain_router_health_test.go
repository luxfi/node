// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRouterHealthSpeaksThroughTheError pins the thing that made a stalled node
// look well: the check computed a verdict, wrote it into the message as
// "healthy": false, and returned a nil error — so the framework, which reads the
// error, passed it. A mainnet chain sat two days without accepting a block while
// /v1/health answered 200 and this check's own message said healthy:false.
func TestRouterHealthSpeaksThroughTheError(t *testing.T) {
	silent := &chainRouter{}
	silent.healthConfig.MinConnectedPeers = 0
	silent.healthConfig.MaxTimeSinceMsgReceived = time.Minute
	silent.lastMsgTime = time.Now().Add(-2 * time.Hour)

	details, err := silent.HealthCheck(context.Background())
	require.Error(t, err, "a router that has heard nothing must fail the check, not just say so")
	require.Contains(t, err.Error(), "no message in")

	m, ok := details.(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, false, m["healthy"], "the message and the error must agree")

	// And the healthy case still passes, so the check is not simply always red.
	live := &chainRouter{}
	live.healthConfig.MinConnectedPeers = 0
	live.healthConfig.MaxTimeSinceMsgReceived = time.Hour
	live.lastMsgTime = time.Now()

	details, err = live.HealthCheck(context.Background())
	require.NoError(t, err)
	m, ok = details.(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, true, m["healthy"])
}
