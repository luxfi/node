// Copyright (C) 2019-2023, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/logging"
)

type CounterHandler struct {
	Tx int
}

func (h *CounterHandler) HandleTx(ids.NodeID, uint32, *Tx) error {
	h.Tx++
	return nil
}

func TestHandleTx(t *testing.T) {
	require := require.New(t)

	handler := CounterHandler{}
	msg := Tx{}

	require.NoError(msg.Handle(&handler, ids.EmptyNodeID, 0))
	require.Equal(1, handler.Tx)
}

func TestNoopHandler(t *testing.T) {
	handler := NoopHandler{
		Log: logging.NoLog{},
	}

	require.NoError(t, handler.HandleTx(ids.EmptyNodeID, 0, nil))
}
