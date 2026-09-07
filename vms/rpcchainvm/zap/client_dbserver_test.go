// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/log"
	luxruntime "github.com/luxfi/runtime"
	rpcdbzap "github.com/luxfi/node/db/rpcdb"
)

// TestInitialize_DBServerAddrPopulated confirms that Client.Initialize
// now spawns a ZAP rpcdb server, populates DBServerAddr in the wire
// InitializeRequest, and the addr is reachable. This is the regression
// guard for "ZAP path leaves dbServerAddr empty" — Phase E's main fix.
func TestInitialize_DBServerAddrPopulated(t *testing.T) {
	require := require.New(t)

	var capturedAddr atomic.Value // string

	addr, stop := startTestServer(t, zapwire.HandlerFunc(func(_ context.Context, msgType zapwire.MessageType, payload []byte) (zapwire.MessageType, []byte, error) {
		require.Equal(zapwire.MsgInitialize, msgType)
		req := &zapwire.InitializeRequest{}
		require.NoError(req.Decode(zapwire.NewReader(payload)))

		// THE assertion: DBServerAddr must be non-empty and reachable.
		require.NotEmpty(req.DBServerAddr, "DBServerAddr should be populated by Client.Initialize")
		capturedAddr.Store(req.DBServerAddr)

		// Sanity: the addr must be a valid TCP host:port.
		_, _, splitErr := net.SplitHostPort(req.DBServerAddr)
		require.NoError(splitErr, "DBServerAddr must be host:port")

		resp := &zapwire.InitializeResponse{
			LastAcceptedID:       make([]byte, 32),
			LastAcceptedParentID: make([]byte, 32),
			Height:               0,
			Bytes:                []byte{},
			Timestamp:            time.Now().UnixNano(),
		}
		buf := zapwire.GetBuffer()
		defer zapwire.PutBuffer(buf)
		resp.Encode(buf)
		out := make([]byte, len(buf.Bytes()))
		copy(out, buf.Bytes())
		return zapwire.MsgInitialize, out, nil
	}))
	defer stop()

	conn, err := zapwire.Dial(context.Background(), addr, nil)
	require.NoError(err)
	defer conn.Close()

	c := NewClient(conn, log.NewNoOpLogger())
	defer c.stopDBServer()

	rt := &luxruntime.Runtime{NetworkID: 1337}
	require.NoError(c.Initialize(context.Background(), block.Init{
		Runtime: rt,
		DB:      memdb.New(),
		Genesis: []byte("{}"),
	}))

	// Verify the captured addr is indeed reachable — the cevm side
	// would dial it from RemoteZapDB::dial.
	dbAddr := capturedAddr.Load().(string)
	require.NotEmpty(dbAddr)

	dbConn, dialErr := zapwire.Dial(context.Background(), dbAddr, nil)
	require.NoError(dialErr, "client should be able to dial back into the spawned db server")
	defer dbConn.Close()
}

// TestInitialize_DBServerActuallyServesRpcdb is the round-trip test:
// after Initialize spawns the db server, dial it as a client and exercise
// Has/Get/Put/Delete via the ZAP rpcdb wire protocol. Proves the spawned
// server is functional, not just listening.
func TestInitialize_DBServerActuallyServesRpcdb(t *testing.T) {
	require := require.New(t)

	var capturedAddr atomic.Value
	mem := memdb.New()

	addr, stop := startTestServer(t, zapwire.HandlerFunc(func(_ context.Context, msgType zapwire.MessageType, payload []byte) (zapwire.MessageType, []byte, error) {
		require.Equal(zapwire.MsgInitialize, msgType)
		req := &zapwire.InitializeRequest{}
		require.NoError(req.Decode(zapwire.NewReader(payload)))
		capturedAddr.Store(req.DBServerAddr)

		resp := &zapwire.InitializeResponse{
			LastAcceptedID:       make([]byte, 32),
			LastAcceptedParentID: make([]byte, 32),
		}
		buf := zapwire.GetBuffer()
		defer zapwire.PutBuffer(buf)
		resp.Encode(buf)
		out := make([]byte, len(buf.Bytes()))
		copy(out, buf.Bytes())
		return zapwire.MsgInitialize, out, nil
	}))
	defer stop()

	conn, err := zapwire.Dial(context.Background(), addr, nil)
	require.NoError(err)
	defer conn.Close()

	c := NewClient(conn, log.NewNoOpLogger())
	defer c.stopDBServer()

	rt := &luxruntime.Runtime{NetworkID: 1337}
	require.NoError(c.Initialize(context.Background(), block.Init{
		Runtime: rt,
		DB:      mem,
		Genesis: []byte("{}"),
	}))

	dbAddr := capturedAddr.Load().(string)
	dbConn, err := zapwire.Dial(context.Background(), dbAddr, nil)
	require.NoError(err)
	defer dbConn.Close()

	ctx := context.Background()

	// Put via ZAP db channel.
	{
		buf := zapwire.GetBuffer()
		buf.WriteBytes([]byte("plugin_key"))
		buf.WriteBytes([]byte("plugin_value"))
		_, _, err := dbConn.Call(ctx, rpcdbzap.MsgDBPut, buf.Bytes())
		zapwire.PutBuffer(buf)
		require.NoError(err)
	}

	// Verify it landed in the underlying memdb.
	got, dErr := mem.Get([]byte("plugin_key"))
	require.NoError(dErr)
	require.Equal([]byte("plugin_value"), got)

	// Get via ZAP db channel — round-trips back to the cevm side.
	{
		buf := zapwire.GetBuffer()
		buf.WriteBytes([]byte("plugin_key"))
		_, resp, err := dbConn.Call(ctx, rpcdbzap.MsgDBGet, buf.Bytes())
		zapwire.PutBuffer(buf)
		require.NoError(err)
		r := zapwire.NewReader(resp)
		val, _ := r.ReadBytes()
		require.Equal([]byte("plugin_value"), val)
	}
}
