// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package getter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/choices"
	"github.com/luxdefi/node/snow/consensus/lux"
	"github.com/luxdefi/node/snow/engine/lux/vertex"
	"github.com/luxdefi/node/snow/engine/common"
	"github.com/luxdefi/node/utils/logging"
	"github.com/luxdefi/node/utils/set"
)

var errUnknownVertex = errors.New("unknown vertex")

func newTest(t *testing.T) (common.AllGetsServer, *vertex.TestManager, *common.SenderTest) {
	manager := vertex.NewTestManager(t)
	manager.Default(true)

	sender := &common.SenderTest{
		T: t,
	}
	sender.Default(true)

	bs, err := New(
		manager,
		sender,
		logging.NoLog{},
		time.Second,
		2000,
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)

	return bs, manager, sender
}

func TestAcceptedFrontier(t *testing.T) {
	require := require.New(t)
	bs, manager, sender := newTest(t)

	vtxID := ids.GenerateTestID()
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{
			vtxID,
		}
	}

	var accepted ids.ID
	sender.SendAcceptedFrontierF = func(_ context.Context, _ ids.NodeID, _ uint32, containerID ids.ID) {
		accepted = containerID
	}
	require.NoError(bs.GetAcceptedFrontier(context.Background(), ids.EmptyNodeID, 0))
	require.Equal(vtxID, accepted)
}

func TestFilterAccepted(t *testing.T) {
	require := require.New(t)
	bs, manager, sender := newTest(t)

	vtxID0 := ids.GenerateTestID()
	vtxID1 := ids.GenerateTestID()
	vtxID2 := ids.GenerateTestID()

	vtx0 := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     vtxID0,
		StatusV: choices.Accepted,
	}}
	vtx1 := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     vtxID1,
		StatusV: choices.Accepted,
	}}

	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (lux.Vertex, error) {
		switch vtxID {
		case vtxID0:
			return vtx0, nil
		case vtxID1:
			return vtx1, nil
		case vtxID2:
			return nil, errUnknownVertex
		}
		require.FailNow(errUnknownVertex.Error())
		return nil, errUnknownVertex
	}

	var accepted []ids.ID
	sender.SendAcceptedF = func(_ context.Context, _ ids.NodeID, _ uint32, frontier []ids.ID) {
		accepted = frontier
	}

	vtxIDs := set.Of(vtxID0, vtxID1, vtxID2)
	require.NoError(bs.GetAccepted(context.Background(), ids.EmptyNodeID, 0, vtxIDs))

	require.Contains(accepted, vtxID0)
	require.Contains(accepted, vtxID1)
	require.NotContains(accepted, vtxID2)
}
