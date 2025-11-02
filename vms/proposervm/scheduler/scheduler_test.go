// Copyright (C) 2019-2025, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/engine/core/common"
	"github.com/luxfi/log"
)

func TestDelayFromNew(t *testing.T) {
	toEngine := make(chan common.Message, 10)
	startTime := time.Now().Add(50 * time.Millisecond)

	s, fromVM := New(log.NoLog{}, toEngine)
	defer s.Close()
	go s.Dispatch(startTime)

	fromVM <- common.Message{Type: common.PendingTxs}

	<-toEngine
	require.LessOrEqual(t, time.Until(startTime), time.Duration(0))
}

func TestDelayFromSetTime(t *testing.T) {
	toEngine := make(chan common.Message, 10)
	now := time.Now()
	startTime := now.Add(50 * time.Millisecond)

	s, fromVM := New(log.NoLog{}, toEngine)
	defer s.Close()
	go s.Dispatch(now)

	s.SetBuildBlockTime(startTime)

	fromVM <- common.Message{Type: common.PendingTxs}

	<-toEngine
	require.LessOrEqual(t, time.Until(startTime), time.Duration(0))
}

func TestReceipt(*testing.T) {
	toEngine := make(chan common.Message, 10)
	now := time.Now()
	startTime := now.Add(50 * time.Millisecond)

	s, fromVM := New(log.NoLog{}, toEngine)
	defer s.Close()
	go s.Dispatch(now)

	fromVM <- common.Message{Type: common.PendingTxs}

	s.SetBuildBlockTime(startTime)

	<-toEngine
}
