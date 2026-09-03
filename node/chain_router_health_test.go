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

// TestRouterHealthWithNoLimitsSet is the configuration every node actually runs:
// nothing assigns RouterHealthConfig, so both limits arrive zero. Comparing a
// duration against a zero limit makes "no message in 649ms, want one within 0s"
// true on a router exchanging messages twice a second — a permanent failure that
// went unnoticed only while the verdict was being discarded. Once the verdict
// rides the error, an unset limit that constrains nothing is the difference
// between a fleet that is ready and a fleet that is never ready.
func TestRouterHealthWithNoLimitsSet(t *testing.T) {
	busy := &chainRouter{} // zero healthConfig, exactly as built today
	busy.lastMsgTime = time.Now().Add(-649 * time.Millisecond)

	details, err := busy.HealthCheck(context.Background())
	require.NoError(t, err, "an unset limit must not fail a router that is exchanging messages")

	m, ok := details.(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, true, m["healthy"])
	require.Equal(t, 0, m["connectedPeers"])

	// A limit that IS set still bites, so this is a fix to the unset case only.
	bounded := &chainRouter{}
	bounded.healthConfig.MaxTimeSinceMsgReceived = time.Second
	bounded.lastMsgTime = time.Now().Add(-time.Hour)

	_, err = bounded.HealthCheck(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "no message in")
}
