// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package gsharedmemory

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/database"
	"github.com/luxfi/node/database/memdb"
	"github.com/luxfi/node/database/prefixdb"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/vms/rpcchainvm/grpcutils"

	sharedmemorypb "github.com/luxfi/node/proto/pb/sharedmemory"
)

func TestInterface(t *testing.T) {
	require := require.New(t)

	chainID0 := ids.GenerateTestID()
	chainID1 := ids.GenerateTestID()

	for _, test := range atomic.SharedMemoryTests {
		baseDB := memdb.New()
		memoryDB := prefixdb.New([]byte{0}, baseDB)
		testDB := prefixdb.New([]byte{1}, baseDB)

		m := atomic.NewMemory(memoryDB)

		sm0, conn0 := wrapSharedMemory(t, m.NewSharedMemory(chainID0), baseDB)
		sm1, conn1 := wrapSharedMemory(t, m.NewSharedMemory(chainID1), baseDB)

		test(t, chainID0, chainID1, sm0, sm1, testDB)

		require.NoError(conn0.Close())
		require.NoError(conn1.Close())
	}
}

func wrapSharedMemory(t *testing.T, sm atomic.SharedMemory, db database.Database) (atomic.SharedMemory, io.Closer) {
	require := require.New(t)

	listener, err := grpcutils.NewListener()
	require.NoError(err)
	serverCloser := grpcutils.ServerCloser{}

	server := grpcutils.NewServer()
	sharedmemorypb.RegisterSharedMemoryServer(server, NewServer(sm, db))
	serverCloser.Add(server)

	go grpcutils.Serve(listener, server)

	conn, err := grpcutils.Dial(listener.Addr().String())
	require.NoError(err)

	rpcsm := NewClient(sharedmemorypb.NewSharedMemoryClient(conn))
	return rpcsm, conn
}
