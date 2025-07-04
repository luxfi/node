// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/snow/engine/common"
	"github.com/luxfi/node/utils/logging"
)

func TestDelayFromNew(t *testing.T) {
	toEngine := make(chan common.Message, 10)
	startTime := time.Now().Add(50 * time.Millisecond)

	s, fromVM := New(logging.NoLog{}, toEngine)
	defer s.Close()
	go s.Dispatch(startTime)

	fromVM <- common.PendingTxs

	<-toEngine
	require.LessOrEqual(t, time.Until(startTime), time.Duration(0))
}

func TestDelayFromSetTime(t *testing.T) {
	toEngine := make(chan common.Message, 10)
	now := time.Now()
	startTime := now.Add(50 * time.Millisecond)

	s, fromVM := New(logging.NoLog{}, toEngine)
	defer s.Close()
	go s.Dispatch(now)

	s.SetBuildBlockTime(startTime)

	fromVM <- common.PendingTxs

	<-toEngine
	require.LessOrEqual(t, time.Until(startTime), time.Duration(0))
}

func TestReceipt(*testing.T) {
	toEngine := make(chan common.Message, 10)
	now := time.Now()
	startTime := now.Add(50 * time.Millisecond)

	s, fromVM := New(logging.NoLog{}, toEngine)
	defer s.Close()
	go s.Dispatch(now)

	fromVM <- common.PendingTxs

	s.SetBuildBlockTime(startTime)

	<-toEngine
}
