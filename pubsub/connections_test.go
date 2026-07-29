// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/log"
)

// TestConstructorsInitializeConnSets pins that both connection sets come back
// from their constructors ready to use. set.Set.Add panics on a nil map, and
// every websocket dial reaches Server.addConnection → conns.Add while the first
// address subscription reaches connections.Add, so an uninitialized set makes
// the /events endpoint panic on its first use.
func TestConstructorsInitializeConnSets(t *testing.T) {
	require := require.New(t)

	conn := &connection{}

	s := New(log.NewNoOpLogger())
	require.NotPanics(func() { s.conns.Add(conn) })
	require.True(s.conns.Contains(conn))

	c := newConnections()
	require.NotPanics(func() { c.Add(conn) })
	require.Equal([]Filter{conn}, c.Conns())
}
